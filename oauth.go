package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"github.com/golang-jwt/jwt/v4"
)

var (
	firestoreClient *firestore.Client
	firebaseAuth    *auth.Client
)

// contextKey is an unexported type for context keys in this package, preventing
// collisions with keys from other packages.
type contextKey int

const claimsKey contextKey = iota

// ClaimsFromContext returns the verified JWT MapClaims stashed by oauthMiddleware.
// Returns false if no claims are present (should not happen in authenticated handlers).
func ClaimsFromContext(ctx context.Context) (jwt.MapClaims, bool) {
	claims, ok := ctx.Value(claimsKey).(jwt.MapClaims)
	return claims, ok
}

// scopesSlice extracts the "scopes" claim as a []string. jwt-go stores JSON
// arrays as []interface{} after parsing, so we convert here.
func scopesSlice(claims jwt.MapClaims) []string {
	raw, _ := claims["scopes"].([]interface{})
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		if str, ok := s.(string); ok {
			out = append(out, str)
		}
	}
	return out
}

// bypassClaims are injected into the context when AUTH_BYPASS=true so that
// downstream handlers (gate, A2A interceptor) behave consistently.
var bypassClaims = jwt.MapClaims{
	"sub":  "bypass-user",
	"type": "access",
	"scopes": []interface{}{
		"lexicon:read", "audio:generate",
		"agent:invoke", "skill:name-generate", "skill:translate", "skill:neologism",
	},
}

// initFirebase initializes the Firebase App, Auth Client, and Firestore Client
// targeting the specific 'mithlond-services' database.
func initFirebase() {
	ctx := context.Background()

	projectID := os.Getenv("FIREBASE_PROJECT_ID")
	if projectID == "" {
		projectID = "testingproject-19c4c"
	}

	databaseID := os.Getenv("FIREBASE_DATABASE")
	if databaseID == "" {
		databaseID = "mithlond-services"
	}

	// Initialize Firebase App
	config := &firebase.Config{
		ProjectID: projectID,
	}
	app, err := firebase.NewApp(ctx, config)
	if err != nil {
		log.Fatalf("Failed to initialize Firebase App: %v", err)
	}

	// Initialize Firebase Auth
	firebaseAuth, err = app.Auth(ctx)
	if err != nil {
		log.Fatalf("Failed to initialize Firebase Auth Client: %v", err)
	}

	// Initialize Cloud Firestore with explicit Database ID (mithlond-services)
	firestoreClient, err = firestore.NewClientWithDatabase(ctx, projectID, databaseID)
	if err != nil {
		log.Fatalf("Failed to initialize Firestore Client: %v", err)
	}

	log.Printf("Successfully initialized Firebase Auth and Firestore database '%s' on project '%s'.", databaseID, projectID)
}

var allowLocalIPs = false

// SafeHTTPClient returns an http.Client equipped with a custom dialer that blocks internal/private IP ranges (SSRF protection).
func SafeHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}

				// 1. Resolve host to IP addresses
				ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
				if err != nil {
					return nil, err
				}

				// 2. Block connection if any resolved IP belongs to a private, loopback, or local-link network
				if !allowLocalIPs {
					for _, ip := range ips {
						if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
							return nil, fmt.Errorf("connection to private/loopback IP blocked: %s", ip)
						}
					}
				}

				// 3. Establish TCP connection safely
				dialer := net.Dialer{
					Timeout: 3 * time.Second,
				}
				return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
			},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "https" {
				return fmt.Errorf("redirect to non-HTTPS scheme blocked: %s", req.URL.Scheme)
			}
			return nil
		},
	}
}

type ClientIDMetadata struct {
	ClientID     string   `json:"client_id"`
	ClientName   string   `json:"client_name"`
	RedirectURIs []string `json:"redirect_uris"`
	JWKSUri      string   `json:"jwks_uri,omitempty"`
}

// FetchAndValidateCIMD fetches the client metadata document and validates it dynamically.
func FetchAndValidateCIMD(ctx context.Context, clientIDUrl string) (*ClientIDMetadata, error) {
	parsedURL, err := url.Parse(clientIDUrl)
	if err != nil {
		return nil, fmt.Errorf("invalid client ID URL format: %w", err)
	}

	// Force HTTPS to guarantee transport encryption and verify TLS certificates
	// Exception for localhost/loopback during local testing
	isLocal := parsedURL.Hostname() == "localhost" || parsedURL.Hostname() == "127.0.0.1"
	if parsedURL.Scheme != "https" && !isLocal {
		return nil, errors.New("client ID URL must use secure https scheme")
	}

	// Append a dynamic query parameter to completely bypass any GFE/outbound intermediate caches
	cacheSep := "?"
	if strings.Contains(clientIDUrl, "?") {
		cacheSep = "&"
	}
	cacheBusterURL := fmt.Sprintf("%s%s_cb=%d", clientIDUrl, cacheSep, time.Now().UnixNano())

	client := SafeHTTPClient()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cacheBusterURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Eldamo-MCP-Server-Auth/1.0")
	// Prevent any caching of metadata retrieval
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch client metadata: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received non-200 status from client domain: %d", resp.StatusCode)
	}

	var meta ClientIDMetadata
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, fmt.Errorf("failed to parse metadata JSON: %w", err)
	}

	// Validation 1: Self-Referential Match
	if meta.ClientID != clientIDUrl {
		return nil, fmt.Errorf("client_id in document (%s) does not match fetched URL (%s)", meta.ClientID, clientIDUrl)
	}

	// Validation 2: Ensure redirect URIs exist
	if len(meta.RedirectURIs) == 0 {
		return nil, errors.New("metadata contains empty redirect_uris array")
	}

	return &meta, nil
}

type AuthCallbackPayload struct {
	IDToken       string `json:"id_token"`
	ClientID      string `json:"client_id"`
	RedirectURI   string `json:"redirect_uri"`
	CodeChallenge string `json:"code_challenge,omitempty"`
}

// compareRedirectURIs checks if the requested URI matches an allowed URI.
// For localhost/loopback redirect URIs, it compares them ignoring the port number (RFC 8252 compliant).
func compareRedirectURIs(requested, allowed string) bool {
	if requested == allowed {
		return true
	}

	reqURL, err := url.Parse(requested)
	if err != nil {
		return false
	}
	allURL, err := url.Parse(allowed)
	if err != nil {
		return false
	}

	// If both are loopback, we compare scheme, path, and host, but ignore port
	isReqLoopback := reqURL.Hostname() == "localhost" || reqURL.Hostname() == "127.0.0.1"
	isAllLoopback := allURL.Hostname() == "localhost" || allURL.Hostname() == "127.0.0.1"

	if isReqLoopback && isAllLoopback {
		return reqURL.Scheme == allURL.Scheme &&
			reqURL.Path == allURL.Path &&
			(reqURL.Hostname() == allURL.Hostname() || 
			 (reqURL.Hostname() == "localhost" && allURL.Hostname() == "127.0.0.1") ||
			 (reqURL.Hostname() == "127.0.0.1" && allURL.Hostname() == "localhost"))
	}

	return false
}

// handleAuthCallback handles secure user authentication callback from the frontend SPA.
func handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload AuthCallbackPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Printf("[OAuth] Failed to decode payload: %v", err)
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	// 1. Verify User ID Token came from our authorized Firebase Frontend SPA
	decodedToken, err := firebaseAuth.VerifyIDToken(r.Context(), payload.IDToken)
	if err != nil {
		log.Printf("[OAuth] ID Token verification failed: %v", err)
		http.Error(w, "Unauthorized user session", http.StatusUnauthorized)
		return
	}

	// 1.5. Check if user is authorized in Firestore
	user, err := getUser(r.Context(), decodedToken.UID)
	
	// Pre-registration logic: If user not found, check if email is pre-registered
	if err != nil {
		// Attempt to find by email
		query := firestoreClient.Collection("authorized_users").Where("email", "==", decodedToken.Claims["email"]).Where("uid", "==", "").Documents(r.Context())
		doc, qErr := query.Next()
		if qErr == nil {
			// Found a pre-registered email, link the UID!
			if _, uErr := doc.Ref.Update(r.Context(), []firestore.Update{
				{Path: "uid", Value: decodedToken.UID},
				{Path: "active", Value: true},
			}); uErr == nil {
				// Re-fetch the now-linked user; this resets the outer err on
				// success so the authorization check below passes.
				user, err = getUser(r.Context(), decodedToken.UID)
			}
		}
	}

	if err != nil || !user.Active {
		log.Printf("[OAuth] User '%s' not authorized or inactive", decodedToken.UID)
		http.Error(w, "User not authorized", http.StatusForbidden)
		return
	}

	userEmail, _ := decodedToken.Claims["email"].(string)
	log.Printf("[OAuth] Authenticated session for user '%s' (%s)", decodedToken.UID, userEmail)

	// 2. Validate Client Identity and Metadata dynamically via CIMD
	clientMeta, err := FetchAndValidateCIMD(r.Context(), payload.ClientID)
	if err != nil {
		log.Printf("[OAuth] Client validation failed for %s: %v", payload.ClientID, err)
		http.Error(w, fmt.Sprintf("Client validation failed: %s", err.Error()), http.StatusForbidden)
		return
	}

	// 3. Confirm requested Redirect URI is explicitly authorized in CIMD
	isRedirectAllowed := false
	for _, uri := range clientMeta.RedirectURIs {
		if compareRedirectURIs(payload.RedirectURI, uri) {
			isRedirectAllowed = true
			break
		}
	}
	if !isRedirectAllowed {
		log.Printf("[OAuth] Redirect URI '%s' not authorized for client '%s'", payload.RedirectURI, payload.ClientID)
		http.Error(w, "Unauthorized redirect URI", http.StatusForbidden)
		return
	}

	// 4. Generate a secure, transient random Authorization Code (32 chars)
	authCode := generateRandomString(32)

	// 5. Store temporary code mapping to Firestore (expires in 5 minutes)
	expiresAt := time.Now().Add(5 * time.Minute)
	_, err = firestoreClient.Collection("mcp_auth_codes").Doc(authCode).Set(r.Context(), map[string]interface{}{
		"client_id":      payload.ClientID,
		"user_uid":       decodedToken.UID,
		"redirect_uri":   payload.RedirectURI,
		"code_challenge": payload.CodeChallenge,
		"expires_at":     expiresAt,
	})
	if err != nil {
		log.Printf("[OAuth] Database write failed: %v", err)
		http.Error(w, "Internal database error", http.StatusInternalServerError)
		return
	}

	// 6. Return code to the frontend SPA so it can redirect the user
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": authCode})
}

// generateRandomString generates a cryptographically secure random string of length n.
func generateRandomString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)[:n]
}

// authorizeScopes is a simple helper to check if a required scope is present in the token's claims.
func authorizeScopes(claims jwt.MapClaims, requiredScope string) bool {
	scopes, ok := claims["scopes"].([]interface{})
	if !ok {
		return false
	}
	for _, s := range scopes {
		if s == requiredScope {
			return true
		}
	}
	return false
}

// getUser retrieves a user document from 'authorized_users'.
func getUser(ctx context.Context, uid string) (*User, error) {
	doc, err := firestoreClient.Collection("authorized_users").Doc(uid).Get(ctx)
	if err != nil {
		return nil, err
	}

	var user User
	if err := doc.DataTo(&user); err != nil {
		return nil, err
	}
	return &user, nil
}

var jwtSigningKey = []byte(getJWTSigningKey())

func getJWTSigningKey() string {
	key := os.Getenv("JWT_SIGNING_KEY")
	if key == "" {
		key = "temporary-dev-signing-key-mithlond"
	}
	return key
}

// generateJWT generates a signed stateless JWT for the given user, client, type, and expiration.
func generateJWT(user *User, clientID, issuer string, duration time.Duration, tokenType string) (string, error) {
	claims := jwt.MapClaims{
		"sub":       user.UID,
		"client_id": clientID,
		"scopes":    user.Scopes,
		"roles":     user.Roles,
		"iss":       issuer,
		"type":      tokenType,
		"iat":       time.Now().Unix(),
		"exp":       time.Now().Add(duration).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSigningKey)
}

// TokenResponse represents the standard OAuth 2.0 token response payload.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// handleTokenExchange exchanges a temporary authorization code or a refresh token for access tokens.
func handleTokenExchange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse parameters (supporting both application/x-www-form-urlencoded and application/json)
	var grantType, code, redirectURI, clientID, codeVerifier, refreshTokenParam string

	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "application/json") {
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
			grantType = payload["grant_type"]
			code = payload["code"]
			redirectURI = payload["redirect_uri"]
			clientID = payload["client_id"]
			codeVerifier = payload["code_verifier"]
			refreshTokenParam = payload["refresh_token"]
		}
	} else {
		// Fallback to form values
		if err := r.ParseForm(); err == nil {
			grantType = r.FormValue("grant_type")
			code = r.FormValue("code")
			redirectURI = r.FormValue("redirect_uri")
			clientID = r.FormValue("client_id")
			codeVerifier = r.FormValue("code_verifier")
			refreshTokenParam = r.FormValue("refresh_token")
		}
	}

	// Dynamic base URL / issuer
	scheme := "https"
	if r.TLS == nil && (strings.HasPrefix(r.Host, "localhost:") || strings.HasPrefix(r.Host, "127.0.0.1:")) {
		scheme = "http"
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	issuer := fmt.Sprintf("%s://%s", scheme, host)

	w.Header().Set("Content-Type", "application/json")

	// Standard error helper
	oauthError := func(status int, errCode, desc string) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":             errCode,
			"error_description": desc,
		})
	}

	switch grantType {
	case "authorization_code":
		if code == "" || clientID == "" {
			oauthError(http.StatusBadRequest, "invalid_request", "Missing required parameters (code, client_id)")
			return
		}

		// 1. Fetch authorization code from Firestore
		docRef := firestoreClient.Collection("mcp_auth_codes").Doc(code)
		docSnap, err := docRef.Get(r.Context())
		if err != nil {
			oauthError(http.StatusBadRequest, "invalid_grant", "Invalid or expired authorization code")
			return
		}

		// Immediate deletion of the authorization code to enforce single-use (RFC 6749 / 2.1 compliance)
		_, _ = docRef.Delete(r.Context())

		data := docSnap.Data()

		// 2. Verify expiration
		expiresAt, ok := data["expires_at"].(time.Time)
		if !ok || time.Now().After(expiresAt) {
			oauthError(http.StatusBadRequest, "invalid_grant", "Authorization code has expired")
			return
		}

		// 3. Verify client_id and redirect_uri parameters match
		storedClientID, _ := data["client_id"].(string)
		if storedClientID != clientID {
			oauthError(http.StatusBadRequest, "invalid_grant", "Client ID mismatch")
			return
		}

		storedRedirectURI, _ := data["redirect_uri"].(string)
		if storedRedirectURI != "" && storedRedirectURI != redirectURI {
			oauthError(http.StatusBadRequest, "invalid_grant", "Redirect URI mismatch")
			return
		}

		// 4. Verify PKCE S256 Challenge
		storedChallenge, _ := data["code_challenge"].(string)
		if storedChallenge != "" {
			if codeVerifier == "" {
				oauthError(http.StatusBadRequest, "invalid_request", "Missing PKCE code_verifier")
				return
			}
			// Compute SHA256 of verifier
			hash := sha256.Sum256([]byte(codeVerifier))
			computedChallenge := base64.RawURLEncoding.EncodeToString(hash[:])
			if computedChallenge != storedChallenge {
				oauthError(http.StatusBadRequest, "invalid_grant", "Invalid PKCE code_verifier")
				return
			}
		}

		userUID, _ := data["user_uid"].(string)

		// Re-fetch user to get latest scopes/roles
		user, err := getUser(r.Context(), userUID)
		if err != nil || !user.Active {
			oauthError(http.StatusForbidden, "invalid_grant", "User not authorized or inactive")
			return
		}

		// 5. Generate Access Token & Refresh Token (JWTs)
		accessToken, err := generateJWT(user, clientID, issuer, 1*time.Hour, "access")
		if err != nil {
			log.Printf("[OAuth] Access Token generation failed: %v", err)
			oauthError(http.StatusInternalServerError, "server_error", "Failed to generate access token")
			return
		}

		refreshToken, err := generateJWT(user, clientID, issuer, 30*24*time.Hour, "refresh")
		if err != nil {
			log.Printf("[OAuth] Refresh Token generation failed: %v", err)
			oauthError(http.StatusInternalServerError, "server_error", "Failed to generate refresh token")
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(TokenResponse{
			AccessToken:  accessToken,
			TokenType:    "Bearer",
			ExpiresIn:    3600,
			RefreshToken: refreshToken,
			Scope:        strings.Join(user.Scopes, " "),
		})
		return

	case "refresh_token":
		if refreshTokenParam == "" {
			oauthError(http.StatusBadRequest, "invalid_request", "Missing refresh_token parameter")
			return
		}

		// Verify signed Refresh Token JWT
		token, err := jwt.Parse(refreshTokenParam, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return jwtSigningKey, nil
		})

		if err != nil || !token.Valid {
			oauthError(http.StatusBadRequest, "invalid_grant", "Invalid or expired refresh token")
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok || claims["type"] != "refresh" {
			oauthError(http.StatusBadRequest, "invalid_grant", "Invalid refresh token type")
			return
		}

		userUID, _ := claims["sub"].(string)
		clientID, _ := claims["client_id"].(string)

		// Re-fetch user to get latest scopes/roles
		user, err := getUser(r.Context(), userUID)
		if err != nil || !user.Active {
			oauthError(http.StatusForbidden, "invalid_grant", "User no longer authorized")
			return
		}

		// Generate fresh Access Token and Refresh Token (rotating the refresh token is standard/safe)
		accessToken, err := generateJWT(user, clientID, issuer, 1*time.Hour, "access")
		if err != nil {
			oauthError(http.StatusInternalServerError, "server_error", "Failed to generate access token")
			return
		}

		newRefreshToken, err := generateJWT(user, clientID, issuer, 30*24*time.Hour, "refresh")
		if err != nil {
			oauthError(http.StatusInternalServerError, "server_error", "Failed to generate refresh token")
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(TokenResponse{
			AccessToken:  accessToken,
			TokenType:    "Bearer",
			ExpiresIn:    3600,
			RefreshToken: newRefreshToken,
			Scope:        strings.Join(user.Scopes, " "),
		})
		return

	default:
		oauthError(http.StatusBadRequest, "unsupported_grant_type", "Supported grant types are 'authorization_code' and 'refresh_token'")
	}
}

// oauthMiddleware is an HTTP middleware that intercepts requests, extracts the
// signed stateless JWT access token, and verifies its signature and validity.
// If AUTH_BYPASS is set to "true", authentication is disabled for local testing.
func oauthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if os.Getenv("AUTH_BYPASS") == "true" {
			log.Printf("[Auth] AUTH_BYPASS enabled, skipping authentication for %s %s", r.Method, r.URL.Path)
			ctx := context.WithValue(r.Context(), claimsKey, bypassClaims)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// 1. Try to extract bearer token from Authorization header
		var tokenStr string
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			if strings.HasPrefix(authHeader, "Bearer ") {
				tokenStr = strings.TrimPrefix(authHeader, "Bearer ")
			}
		}

		// 2. Fallback to extracting token from URL query parameters (resilient for standard SSE connections)
		if tokenStr == "" {
			tokenStr = r.URL.Query().Get("token")
			if tokenStr == "" {
				tokenStr = r.URL.Query().Get("access_token")
			}
		}

		if tokenStr == "" {
			// Print headers for debugging purposes to see if the client sent the token in an unexpected way
			log.Printf("[Auth] Debug Headers for %s %s: %+v", r.Method, r.URL.Path, r.Header)

			log.Printf("[Auth] Rejected request %s %s: Missing access token", r.Method, r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			// Add standard WWW-Authenticate header to signal OAuth 2.1 authentication requirement
			w.Header().Set("WWW-Authenticate", `Bearer realm="Mithlond", error="unauthorized", error_description="Missing access token"`)
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":             "unauthorized",
				"error_description": "Missing access token in Authorization header or query parameter",
			})
			return
		}

		// 3. Verify and parse signed Access Token JWT
		token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return jwtSigningKey, nil
		})

		if err != nil || !token.Valid {
			log.Printf("[Auth] Rejected request %s %s: Invalid or expired token: %v", r.Method, r.URL.Path, err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":             "invalid_token",
				"error_description": "The provided access token is invalid or expired",
			})
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok || claims["type"] != "access" {
			log.Printf("[Auth] Rejected request %s %s: Token claims are invalid", r.Method, r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":             "invalid_token",
				"error_description": "Invalid token type or claims",
			})
			return
		}

		// Signature and expiration are valid. Stash the claims in the request
		// context so downstream handlers (gate, A2A CallInterceptor) can read
		// them without re-parsing the token.
		ctx := context.WithValue(r.Context(), claimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
