package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"github.com/golang-jwt/jwt/v4"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "eldamo-admin",
	Short: "Admin tool for Eldamo MCP Server",
}

func getFirebaseApp() (*firebase.App, error) {
	ctx := context.Background()
	pID := os.Getenv("FIREBASE_PROJECT_ID")
	if pID == "" {
		pID = "testingproject-19c4c"
	}
	config := &firebase.Config{ProjectID: pID}
	return firebase.NewApp(ctx, config)
}

func getFirestoreClient() (*firestore.Client, error) {
	ctx := context.Background()
	pID := os.Getenv("FIREBASE_PROJECT_ID")
	dID := os.Getenv("FIREBASE_DATABASE")
	if pID == "" || dID == "" {
		return nil, fmt.Errorf("FIREBASE_PROJECT_ID and FIREBASE_DATABASE must be set")
	}
	config := &firebase.Config{ProjectID: pID}
	_, err := firebase.NewApp(ctx, config)
	if err != nil {
		return nil, err
	}
	return firestore.NewClientWithDatabase(ctx, pID, dID)
}

func getFirebaseAuthClient() (*auth.Client, error) {
	app, err := getFirebaseApp()
	if err != nil {
		return nil, err
	}
	return app.Auth(context.Background())
}

func main() {
	rootCmd.AddCommand(listCmd, addCmd, preRegisterCmd, grantCmd, revokeScopeCmd, grantAllCmd, tokenCmd)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// defaultUserScopes is the canonical baseline scope set granted to every new
// user. Keep this in sync with allScopes in a2a.go.
var defaultUserScopes = []string{
	"lexicon:read",
	"audio:generate",
	"agent:invoke",
	"skill:name-generate",
	"skill:translate",
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all authorized users",
	Run: func(cmd *cobra.Command, args []string) {
		client, err := getFirestoreClient()
		if err != nil { log.Fatal(err) }
		defer func() { _ = client.Close() }()
		
		iter := client.Collection("authorized_users").Documents(context.Background())
		for {
			doc, err := iter.Next()
			if err != nil { break }
			fmt.Printf("%s: %v\n", doc.Ref.ID, doc.Data())
		}
	},
}

var addCmd = &cobra.Command{
	Use:   "add <uid> <email>",
	Short: "Add a new user by their specific Firebase UID",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		client, err := getFirestoreClient()
		if err != nil { log.Fatal(err) }
		defer func() { _ = client.Close() }()
		
		uid, email := args[0], args[1]
		_, err = client.Collection("authorized_users").Doc(uid).Set(context.Background(), map[string]interface{}{
			"uid": uid, "email": email, "active": true, "roles": []string{"user"},
			"scopes": defaultUserScopes,
		})
		if err != nil { log.Fatal(err) }
		fmt.Printf("Added %s\n", email)
	},
}

var preRegisterCmd = &cobra.Command{
	Use:   "pre-register <email>",
	Short: "Pre-register a user by email (even if they haven't logged in yet)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		email := args[0]
		client, err := getFirestoreClient()
		if err != nil { log.Fatal(err) }
		defer func() { _ = client.Close() }()
		
		_, err = client.Collection("authorized_users").Doc(email).Set(context.Background(), map[string]interface{}{
			"uid":    "", // Placeholder
			"email":  email,
			"active": false, // Inactive until first login
			"roles":  []string{"user"},
			"scopes": defaultUserScopes,
		})
		if err != nil { log.Fatal(err) }
		fmt.Printf("User '%s' pre-registered. They will be activated upon first login.\n", email)
	},
}

var grantCmd = &cobra.Command{
	Use:   "grant <uid-or-email> <scope>",
	Short: "Grant a specific scope to a user",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		client, err := getFirestoreClient()
		if err != nil { log.Fatal(err) }
		defer func() { _ = client.Close() }()

		uid, err := resolveUID(context.Background(), args[0])
		if err != nil { log.Fatal(err) }
		scope := args[1]

		_, err = client.Collection("authorized_users").Doc(uid).Update(context.Background(), []firestore.Update{
			{Path: "scopes", Value: firestore.ArrayUnion(scope)},
		})
		if err != nil { log.Fatalf("Failed to grant scope: %v", err) }
		fmt.Printf("Granted scope '%s' to user %s\n", scope, uid)
	},
}

var revokeScopeCmd = &cobra.Command{
	Use:   "revoke-scope <uid-or-email> <scope>",
	Short: "Revoke a specific scope from a user",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		client, err := getFirestoreClient()
		if err != nil { log.Fatal(err) }
		defer func() { _ = client.Close() }()

		uid, err := resolveUID(context.Background(), args[0])
		if err != nil { log.Fatal(err) }
		scope := args[1]

		_, err = client.Collection("authorized_users").Doc(uid).Update(context.Background(), []firestore.Update{
			{Path: "scopes", Value: firestore.ArrayRemove(scope)},
		})
		if err != nil { log.Fatalf("Failed to revoke scope: %v", err) }
		fmt.Printf("Revoked scope '%s' from user %s\n", scope, uid)
	},
}

// grantAllCmd grants one or more scopes to every user in authorized_users using
// ArrayUnion — idempotent and safe to re-run. Use this as the deploy-day
// migration step whenever a new scope is added to defaultUserScopes.
//
// Usage: eldamo-admin grant-all <scope> [scope...]
// Example: eldamo-admin grant-all skill:name-generate skill:translate
var grantAllCmd = &cobra.Command{
	Use:   "grant-all <scope> [scope...]",
	Short: "Grant one or more scopes to ALL authorized users (idempotent)",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		client, err := getFirestoreClient()
		if err != nil {
			log.Fatal(err)
		}
		defer func() { _ = client.Close() }()

		ctx := context.Background()

		// Convert scope strings to []interface{} for ArrayUnion.
		unionValues := make([]interface{}, len(args))
		for i, s := range args {
			unionValues[i] = s
		}
		update := []firestore.Update{
			{Path: "scopes", Value: firestore.ArrayUnion(unionValues...)},
		}

		iter := client.Collection("authorized_users").Documents(ctx)
		updated := 0
		for {
			doc, err := iter.Next()
			if err != nil {
				break
			}
			if _, err = doc.Ref.Update(ctx, update); err != nil {
				log.Printf("Failed to update %s: %v", doc.Ref.ID, err)
				continue
			}
			updated++
			fmt.Printf("  ✓ %s\n", doc.Ref.ID)
		}
		fmt.Printf("Granted %v to %d user(s).\n", args, updated)
	},
}

var tokenCmd = &cobra.Command{
	Use:   "token <uid>",
	Short: "Generate a JWT for testing",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		token, err := generateToken(args[0])
		if err != nil { log.Fatal(err) }
		fmt.Println(token)
	},
}

func generateToken(uid string) (string, error) {
	key := os.Getenv("JWT_SIGNING_KEY")
	if key == "" {
		key = "temporary-dev-signing-key-mithlond"
	}
	
	claims := jwt.MapClaims{
		"sub": uid,
		"scopes": []string{
			"lexicon:read", "audio:generate",
			"agent:invoke", "skill:name-generate", "skill:translate",
		},
		"exp":  time.Now().Add(1 * time.Hour).Unix(),
		"type": "access",
	}
	
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(key))
}

func resolveUID(ctx context.Context, input string) (string, error) {
	if !strings.Contains(input, "@") {
		return input, nil 
	}

	authClient, err := getFirebaseAuthClient()
	if err != nil {
		return "", err
	}

	userRecord, err := authClient.GetUserByEmail(ctx, input)
	if err != nil {
		return "", fmt.Errorf("could not find UID for email %s: %w", input, err)
	}
	return userRecord.UID, nil
}
