package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

func main() {
	// 1. Define command line flags for administrator convenience
	subFlag := flag.String("sub", "dev-user-123", "The subject/UID of the token (e.g., user email or developer ID)")
	durationFlag := flag.String("duration", "1h", "The duration of the token (e.g., '1h', '24h', or '30d' for 30 days)")
	typeFlag := flag.String("type", "access", "The type of token ('access' or 'refresh')")
	flag.Parse()

	// 2. Parse duration
	var duration time.Duration
	var err error
	if strings.HasSuffix(*durationFlag, "d") {
		// Custom parser for "d" suffix since Go's time.ParseDuration doesn't support days natively
		daysStr := strings.TrimSuffix(*durationFlag, "d")
		var days int
		_, err = fmt.Sscanf(daysStr, "%d", &days)
		if err == nil {
			duration = time.Duration(days) * 24 * time.Hour
		}
	} else {
		duration, err = time.ParseDuration(*durationFlag)
	}

	if err != nil || duration == 0 {
		log.Fatalf("Invalid duration format: '%s'. Use standard Go formats like '1h', '24h', or '30d'.", *durationFlag)
	}

	// 3. Read .env if it exists to retrieve the JWT_SIGNING_KEY
	signingKey := "temporary-dev-signing-key-mithlond"
	file, err := os.Open(".env")
	if err == nil {
		defer func() {
			_ = file.Close()
		}()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "JWT_SIGNING_KEY=") {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					signingKey = strings.Trim(parts[1], " '\"")
				}
			}
		}
	}

	// 4. Generate signed token with dynamic flags
	claims := jwt.MapClaims{
		"sub":       *subFlag,
		"client_id": "https://www.mithlond.com/mcp-client-metadata.json",
		"scope":     "mcp",
		"iss":       "https://www.mithlond.com",
		"type":      *typeFlag,
		"iat":       time.Now().Unix(),
		"exp":       time.Now().Add(duration).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString([]byte(signingKey))
	if err != nil {
		log.Fatalf("Failed to sign token: %v", err)
	}

	fmt.Println(tokenStr)
}
