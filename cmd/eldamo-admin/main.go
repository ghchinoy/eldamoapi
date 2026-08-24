package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"github.com/golang-jwt/jwt/v4"
	"github.com/spf13/cobra"
)

var (
	// Brand / Accent Colors
	colorEmerald = lipgloss.Color("#10B981")
	colorGold    = lipgloss.Color("#DCB386")
	colorSlate   = lipgloss.Color("#64748B")
	colorBorder  = lipgloss.Color("#334155")

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorEmerald).
			MarginTop(1).
			MarginBottom(1)

	cardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorEmerald).
			Padding(0, 1).
			Width(80).
			MarginTop(1).
			MarginBottom(1)

	badgeActive = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#10B981")).
			Background(lipgloss.Color("#064E3B")).
			Padding(0, 1).
			Render("ACTIVE")

	badgePending = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#F59E0B")).
			Background(lipgloss.Color("#78350F")).
			Padding(0, 1).
			Render("PENDING")

	badgeApproved = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#38BDF8")).
			Background(lipgloss.Color("#0C4A6E")).
			Padding(0, 1).
			Render("APPROVED")

	badgeInactive = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#94A3B8")).
			Background(lipgloss.Color("#1E293B")).
			Padding(0, 1).
			Render("INACTIVE")

	labelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorSlate).
			Width(14)

	valueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F8FAFC"))

	goldStyle = lipgloss.NewStyle().
			Foreground(colorGold).
			Bold(true)

	subtleStyle = lipgloss.NewStyle().
			Foreground(colorSlate).
			Italic(true)

	tableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#F8FAFC")).
				Background(lipgloss.Color("#1E293B")).
				Padding(0, 1)

	tableCellStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#CBD5E1")).
			Padding(0, 1)
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
	if pID == "" {
		pID = "testingproject-19c4c"
	}
	dID := os.Getenv("FIREBASE_DATABASE")
	if dID == "" {
		dID = "mithlond-services"
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
	requestsCmd.AddCommand(requestsListCmd, requestsApproveCmd)
	rootCmd.AddCommand(listCmd, addCmd, preRegisterCmd, grantCmd, revokeScopeCmd, grantAllCmd, tokenCmd, requestsCmd)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// minimalDefaultScopes is the baseline scope tier granted to newly approved /
// onboarded users. It includes all free, deterministic, in-memory capabilities
// (lexicon search, A2A messaging, deterministic name generation).
// Cost-bearing capabilities (LLM skills like translate/neologism, and audio TTS)
// require an explicit upgrade via grant-scope.
var minimalDefaultScopes = []string{
	"lexicon:read",
	"agent:invoke",
	"skill:name-generate",
}

// allUserScopes contains the full capability set supported by this server.
var allUserScopes = []string{
	"lexicon:read",
	"audio:generate",
	"agent:invoke",
	"skill:name-generate",
	"skill:translate",
	"skill:neologism",
}

// defaultUserScopes is the canonical baseline scope set granted to every new
// user on onboarding/approval.
var defaultUserScopes = minimalDefaultScopes

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all authorized users",
	Run: func(cmd *cobra.Command, args []string) {
		client, err := getFirestoreClient()
		if err != nil {
			log.Fatal(err)
		}
		defer func() { _ = client.Close() }()

		iter := client.Collection("authorized_users").Documents(context.Background())

		var rows [][]string
		activeCount := 0
		inactiveCount := 0

		for {
			doc, err := iter.Next()
			if err != nil {
				break
			}
			data := doc.Data()

			active, _ := data["active"].(bool)
			email, _ := data["email"].(string)
			uid, _ := data["uid"].(string)
			roles, _ := data["roles"].([]interface{})
			scopes, _ := data["scopes"].([]interface{})

			statusStr := badgeInactive
			if active {
				statusStr = badgeActive
				activeCount++
			} else {
				inactiveCount++
			}

			if email == "" {
				email = doc.Ref.ID
			}

			uidDisplay := uid
			if uidDisplay == "" {
				uidDisplay = subtleStyle.Render("— (pre-registered)")
			}

			var scopeList []string
			for _, s := range scopes {
				scopeList = append(scopeList, fmt.Sprintf("%v", s))
			}
			scopeDisplay := fmt.Sprintf("%d scopes", len(scopeList))
			if len(scopeList) > 0 {
				scopeDisplay = fmt.Sprintf("%d (%s)", len(scopeList), strings.Join(scopeList, ", "))
			}

			var roleList []string
			for _, r := range roles {
				roleList = append(roleList, fmt.Sprintf("%v", r))
			}
			roleDisplay := strings.Join(roleList, ", ")
			if roleDisplay == "" {
				roleDisplay = "user"
			}

			rows = append(rows, []string{
				statusStr,
				email,
				uidDisplay,
				scopeDisplay,
				roleDisplay,
			})
		}

		if len(rows) == 0 {
			fmt.Println(titleStyle.Render("🏹 Eldamo Authorized Users"))
			fmt.Println(subtleStyle.Render("No authorized users found in authorized_users collection."))
			return
		}

		t := table.New().
			Border(lipgloss.RoundedBorder()).
			BorderStyle(lipgloss.NewStyle().Foreground(colorBorder)).
			Headers("STATUS", "EMAIL / IDENTIFIER", "FIREBASE UID", "SCOPES", "ROLES").
			Rows(rows...)

		t.StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return tableHeaderStyle
			}
			return tableCellStyle
		})

		fmt.Println(titleStyle.Render("🏹 Eldamo Authorized Users Directory"))
		fmt.Println(t.Render())
		fmt.Printf("%s\n\n", subtleStyle.Render(fmt.Sprintf("Total: %d users (%d active, %d pre-registered)", len(rows), activeCount, inactiveCount)))
	},
}

var requestsCmd = &cobra.Command{
	Use:   "requests",
	Short: "Manage incoming workspace access requests",
}

var requestsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List incoming workspace access requests",
	Run: func(cmd *cobra.Command, args []string) {
		client, err := getFirestoreClient()
		if err != nil {
			log.Fatal(err)
		}
		defer func() { _ = client.Close() }()

		iter := client.Collection("access_requests").Documents(context.Background())

		var rows [][]string
		pendingCount := 0
		approvedCount := 0

		for {
			doc, err := iter.Next()
			if err != nil {
				break
			}
			data := doc.Data()

			status, _ := data["status"].(string)
			email, _ := data["email"].(string)
			displayName, _ := data["displayName"].(string)
			tools, _ := data["tools"].([]interface{})
			notes, _ := data["notes"].(string)

			statusStr := badgePending
			if strings.EqualFold(status, "approved") {
				statusStr = badgeApproved
				approvedCount++
			} else {
				pendingCount++
			}

			var toolList []string
			for _, t := range tools {
				toolList = append(toolList, fmt.Sprintf("%v", t))
			}
			toolDisplay := strings.Join(toolList, ", ")
			if toolDisplay == "" {
				toolDisplay = subtleStyle.Render("All")
			}

			notesDisplay := notes
			if notesDisplay == "" {
				notesDisplay = subtleStyle.Render("None")
			} else if len(notesDisplay) > 40 {
				notesDisplay = notesDisplay[:37] + "..."
			}

			nameDisplay := displayName
			if nameDisplay == "" {
				nameDisplay = subtleStyle.Render("—")
			}

			rows = append(rows, []string{
				statusStr,
				email,
				nameDisplay,
				doc.Ref.ID,
				toolDisplay,
				notesDisplay,
			})
		}

		if len(rows) == 0 {
			fmt.Println(titleStyle.Render("📬 Incoming Workspace Access Requests"))
			fmt.Println(subtleStyle.Render("No access requests found in access_requests collection."))
			return
		}

		t := table.New().
			Border(lipgloss.RoundedBorder()).
			BorderStyle(lipgloss.NewStyle().Foreground(colorBorder)).
			Headers("STATUS", "EMAIL", "DISPLAY NAME", "FIREBASE UID", "REQUESTED TOOLS", "NOTES").
			Rows(rows...)

		t.StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return tableHeaderStyle
			}
			return tableCellStyle
		})

		fmt.Println(titleStyle.Render("📬 Workspace Access Requests"))
		fmt.Println(t.Render())
		fmt.Printf("%s\n\n", subtleStyle.Render(fmt.Sprintf("Total: %d requests (%d pending review, %d approved)", len(rows), pendingCount, approvedCount)))
	},
}

var requestsApproveCmd = &cobra.Command{
	Use:   "approve <uid>",
	Short: "Approve an access request and activate user in authorized_users",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		client, err := getFirestoreClient()
		if err != nil {
			log.Fatal(err)
		}
		defer func() { _ = client.Close() }()

		ctx := context.Background()
		uid := args[0]
		reqDoc, err := client.Collection("access_requests").Doc(uid).Get(ctx)
		if err != nil {
			log.Fatalf("Failed to find access request for UID '%s': %v", uid, err)
		}
		data := reqDoc.Data()
		email, _ := data["email"].(string)
		displayName, _ := data["displayName"].(string)

		// 1. Add to authorized_users with active: true and defaultUserScopes
		_, err = client.Collection("authorized_users").Doc(uid).Set(ctx, map[string]interface{}{
			"uid":         uid,
			"email":       email,
			"displayName": displayName,
			"active":      true,
			"roles":       []string{"user"},
			"scopes":      defaultUserScopes,
		})
		if err != nil {
			log.Fatalf("Failed to add user to authorized_users: %v", err)
		}

		// 2. Mark request as approved
		_, _ = client.Collection("access_requests").Doc(uid).Update(ctx, []firestore.Update{
			{Path: "status", Value: "approved"},
		})

		content := fmt.Sprintf(
			"%s\n\n%s %s\n%s %s\n%s %s\n%s %s\n%s %s\n",
			titleStyle.Render("✓ Access Request Approved & User Activated"),
			labelStyle.Render("User Email:"), valueStyle.Render(email),
			labelStyle.Render("Display Name:"), valueStyle.Render(displayName),
			labelStyle.Render("Firebase UID:"), goldStyle.Render(uid),
			labelStyle.Render("Status:"), badgeActive,
			labelStyle.Render("Scopes:"), subtleStyle.Render(strings.Join(defaultUserScopes, ", ")),
		)
		fmt.Println(cardStyle.Render(content))
	},
}

var addCmd = &cobra.Command{
	Use:   "add <uid> <email>",
	Short: "Add a new user by their specific Firebase UID",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		client, err := getFirestoreClient()
		if err != nil {
			log.Fatal(err)
		}
		defer func() { _ = client.Close() }()

		uid, email := args[0], args[1]
		_, err = client.Collection("authorized_users").Doc(uid).Set(context.Background(), map[string]interface{}{
			"uid":    uid,
			"email":  email,
			"active": true,
			"roles":  []string{"user"},
			"scopes": defaultUserScopes,
		})
		if err != nil {
			log.Fatal(err)
		}

		content := fmt.Sprintf(
			"%s\n\n%s %s\n%s %s\n%s %s\n%s %s\n",
			titleStyle.Render("✓ User Successfully Added"),
			labelStyle.Render("User Email:"), valueStyle.Render(email),
			labelStyle.Render("Firebase UID:"), goldStyle.Render(uid),
			labelStyle.Render("Status:"), badgeActive,
			labelStyle.Render("Scopes:"), subtleStyle.Render(strings.Join(defaultUserScopes, ", ")),
		)
		fmt.Println(cardStyle.Render(content))
	},
}

var preRegisterCmd = &cobra.Command{
	Use:   "pre-register <email>",
	Short: "Pre-register a user by email (activated upon first login)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		email := args[0]
		client, err := getFirestoreClient()
		if err != nil {
			log.Fatal(err)
		}
		defer func() { _ = client.Close() }()

		_, err = client.Collection("authorized_users").Doc(email).Set(context.Background(), map[string]interface{}{
			"uid":    "",
			"email":  email,
			"active": false,
			"roles":  []string{"user"},
			"scopes": defaultUserScopes,
		})
		if err != nil {
			log.Fatal(err)
		}

		content := fmt.Sprintf(
			"%s\n\n%s %s\n%s %s\n%s %s\n",
			titleStyle.Render("✓ User Pre-Registered"),
			labelStyle.Render("User Email:"), valueStyle.Render(email),
			labelStyle.Render("Status:"), badgeInactive,
			labelStyle.Render("Activation:"), subtleStyle.Render("Will activate automatically upon first login at /mcp-auth"),
		)
		fmt.Println(cardStyle.Render(content))
	},
}

var grantCmd = &cobra.Command{
	Use:   "grant <uid-or-email> <scope>",
	Short: "Grant a specific scope to a user",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		client, err := getFirestoreClient()
		if err != nil {
			log.Fatal(err)
		}
		defer func() { _ = client.Close() }()

		uid, err := resolveUID(context.Background(), args[0])
		if err != nil {
			log.Fatal(err)
		}
		scope := args[1]

		_, err = client.Collection("authorized_users").Doc(uid).Update(context.Background(), []firestore.Update{
			{Path: "scopes", Value: firestore.ArrayUnion(scope)},
		})
		if err != nil {
			log.Fatalf("Failed to grant scope: %v", err)
		}

		fmt.Printf("✓ Granted scope '%s' to user %s\n", goldStyle.Render(scope), valueStyle.Render(uid))
	},
}

var revokeScopeCmd = &cobra.Command{
	Use:   "revoke-scope <uid-or-email> <scope>",
	Short: "Revoke a specific scope from a user",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		client, err := getFirestoreClient()
		if err != nil {
			log.Fatal(err)
		}
		defer func() { _ = client.Close() }()

		uid, err := resolveUID(context.Background(), args[0])
		if err != nil {
			log.Fatal(err)
		}
		scope := args[1]

		_, err = client.Collection("authorized_users").Doc(uid).Update(context.Background(), []firestore.Update{
			{Path: "scopes", Value: firestore.ArrayRemove(scope)},
		})
		if err != nil {
			log.Fatalf("Failed to revoke scope: %v", err)
		}

		fmt.Printf("✓ Revoked scope '%s' from user %s\n", goldStyle.Render(scope), valueStyle.Render(uid))
	},
}

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
		fmt.Printf("\n%s\n", titleStyle.Render(fmt.Sprintf("✓ Granted scopes %v to %d user(s)", args, updated)))
	},
}

var tokenCmd = &cobra.Command{
	Use:   "token <uid>",
	Short: "Generate a JWT for testing",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		uid := args[0]
		token, err := generateToken(uid)
		if err != nil {
			log.Fatal(err)
		}

		tokenBox := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A7F3D0")).
			Background(lipgloss.Color("#020617")).
			Padding(1).
			Width(74).
			Render(token)

		content := fmt.Sprintf(
			"%s\n\n%s %s\n%s %s\n%s %s\n\n%s\n%s",
			titleStyle.Render("🔑 Mithlond Access Token"),
			labelStyle.Render("Subject:"), goldStyle.Render(uid),
			labelStyle.Render("Expires:"), valueStyle.Render("1 hour (stateless HMAC-SHA256)"),
			labelStyle.Render("Scopes:"), subtleStyle.Render(strings.Join(allUserScopes, ", ")),
			labelStyle.Render("Token:"),
			tokenBox,
		)
		fmt.Println(cardStyle.Render(content))
	},
}

func generateToken(uid string) (string, error) {
	key := os.Getenv("JWT_SIGNING_KEY")
	if key == "" {
		key = "temporary-dev-signing-key-mithlond"
	}

	claims := jwt.MapClaims{
		"sub":    uid,
		"scopes": allUserScopes,
		"exp":    time.Now().Add(1 * time.Hour).Unix(),
		"type":   "access",
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
