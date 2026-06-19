package main

import (
	"context"
	"fmt"
	"os"

	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"github.com/spf13/cobra"
)

var (
	rootCmd = &cobra.Command{
		Use:   "eldamo-admin",
		Short: "Admin tool for Eldamo MCP Server",
	}
	projectID, databaseID string
)

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
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
