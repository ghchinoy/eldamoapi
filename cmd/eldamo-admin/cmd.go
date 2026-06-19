package main

import (
	"context"
	"fmt"
	"log"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(addEmailCmd) // Add the new email-based search cmd
	rootCmd.AddCommand(tokenCmd)
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all authorized users",
	Run: func(cmd *cobra.Command, args []string) {
		client, err := getFirestoreClient()
		if err != nil { log.Fatal(err) }
		defer client.Close()
		
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
		defer client.Close()
		
		uid, email := args[0], args[1]
		_, err = client.Collection("authorized_users").Doc(uid).Set(context.Background(), map[string]interface{}{
			"uid": uid, "email": email, "active": true, "roles": []string{"user"}, "scopes": []string{"lexicon:read"},
		})
		if err != nil { log.Fatal(err) }
		fmt.Printf("Added %s\n", email)
	},
}

var addEmailCmd = &cobra.Command{
	Use:   "add-email <email>",
	Short: "Add a new user by looking up their email in Firebase Auth",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		email := args[0]

		// 1. Resolve UID from Firebase Auth by email lookup
		authClient, err := getFirebaseAuthClient()
		if err != nil { log.Fatalf("Failed to initialize Auth Client: %v", err) }
		
		userRecord, err := authClient.GetUserByEmail(context.Background(), email)
		if err != nil {
			log.Fatalf("Failed to find user with email '%s' in Firebase Auth. Ensure they have authenticated/logged in at least once: %v", email, err)
		}

		// 2. Write authorized user record to Firestore
		client, err := getFirestoreClient()
		if err != nil { log.Fatal(err) }
		defer client.Close()
		
		_, err = client.Collection("authorized_users").Doc(userRecord.UID).Set(context.Background(), map[string]interface{}{
			"uid":    userRecord.UID,
			"email":  email,
			"active": true,
			"roles":  []string{"user"},
			"scopes": []string{"lexicon:read", "audio:generate"},
		})
		if err != nil { log.Fatal(err) }
		fmt.Printf("User '%s' found with UID '%s' and successfully authorized in Firestore.\n", email, userRecord.UID)
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
