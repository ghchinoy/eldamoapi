package main

import (
	"os"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

func generateToken(uid string) (string, error) {
	key := os.Getenv("JWT_SIGNING_KEY")
	if key == "" {
		key = "temporary-dev-signing-key-mithlond"
	}
	
	claims := jwt.MapClaims{
		"sub":    uid,
		"scopes": []string{"lexicon:read", "audio:generate"},
		"exp":    time.Now().Add(1 * time.Hour).Unix(),
		"type":   "access",
	}
	
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(key))
}
