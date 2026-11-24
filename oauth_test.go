package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

// makeTestToken signs a token with the specified method, key, and claims.
func makeTestToken(method jwt.SigningMethod, key any, claims jwt.MapClaims) string {
	token := jwt.NewWithClaims(method, claims)
	s, err := token.SignedString(key)
	if err != nil {
		panic(err)
	}
	return s
}

func TestOAuthMiddleware_SecurityAndAlgorithmPinning(t *testing.T) {
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := ClaimsFromContext(r.Context())
		if !ok {
			t.Error("expected claims in context")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"sub":    claims["sub"],
		})
	})

	secured := oauthMiddleware(innerHandler)

	t.Run("valid HS256 access token succeeds", func(t *testing.T) {
		validToken := makeTestToken(jwt.SigningMethodHS256, jwtSigningKey, jwt.MapClaims{
			"sub":    "valid-user",
			"type":   "access",
			"scopes": []string{"lexicon:read"},
			"exp":    time.Now().Add(10 * time.Minute).Unix(),
			"iat":    time.Now().Unix(),
		})

		req := httptest.NewRequest("GET", "/sse", nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		w := httptest.NewRecorder()

		secured.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("missing token returns 401 with RFC 9728 header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/sse", nil)
		w := httptest.NewRecorder()

		secured.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized, got %d", w.Code)
		}
		wwwAuth := w.Header().Get("WWW-Authenticate")
		if !strings.Contains(wwwAuth, "resource_metadata=") {
			t.Errorf("expected WWW-Authenticate header to contain resource_metadata pointer, got: %s", wwwAuth)
		}
	})

	t.Run("alg:none token is strictly rejected", func(t *testing.T) {
		noneToken := makeTestToken(jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, jwt.MapClaims{
			"sub":    "attacker",
			"type":   "access",
			"scopes": []string{"lexicon:read", "audio:generate"},
			"exp":    time.Now().Add(10 * time.Minute).Unix(),
		})

		req := httptest.NewRequest("GET", "/sse", nil)
		req.Header.Set("Authorization", "Bearer "+noneToken)
		w := httptest.NewRecorder()

		secured.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized for alg:none, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("RS256 asymmetric token is strictly rejected (algorithm confusion prevention)", func(t *testing.T) {
		rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("failed to generate RSA key: %v", err)
		}

		rsToken := makeTestToken(jwt.SigningMethodRS256, rsaKey, jwt.MapClaims{
			"sub":    "attacker",
			"type":   "access",
			"scopes": []string{"lexicon:read"},
			"exp":    time.Now().Add(10 * time.Minute).Unix(),
		})

		req := httptest.NewRequest("GET", "/sse", nil)
		req.Header.Set("Authorization", "Bearer "+rsToken)
		w := httptest.NewRecorder()

		secured.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized for RS256 token, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("HS384 / HS512 token rejected by WithValidMethods filter", func(t *testing.T) {
		hs512Token := makeTestToken(jwt.SigningMethodHS512, jwtSigningKey, jwt.MapClaims{
			"sub":    "user",
			"type":   "access",
			"scopes": []string{"lexicon:read"},
			"exp":    time.Now().Add(10 * time.Minute).Unix(),
		})

		req := httptest.NewRequest("GET", "/sse", nil)
		req.Header.Set("Authorization", "Bearer "+hs512Token)
		w := httptest.NewRecorder()

		secured.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized for HS512 token, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("token signed with wrong key is rejected", func(t *testing.T) {
		wrongKeyToken := makeTestToken(jwt.SigningMethodHS256, []byte("wrong-signing-key-01234567890123"), jwt.MapClaims{
			"sub":    "user",
			"type":   "access",
			"scopes": []string{"lexicon:read"},
			"exp":    time.Now().Add(10 * time.Minute).Unix(),
		})

		req := httptest.NewRequest("GET", "/sse", nil)
		req.Header.Set("Authorization", "Bearer "+wrongKeyToken)
		w := httptest.NewRecorder()

		secured.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized for wrong key, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("expired token is rejected", func(t *testing.T) {
		expiredToken := makeTestToken(jwt.SigningMethodHS256, jwtSigningKey, jwt.MapClaims{
			"sub":    "user",
			"type":   "access",
			"scopes": []string{"lexicon:read"},
			"exp":    time.Now().Add(-10 * time.Minute).Unix(), // in the past
		})

		req := httptest.NewRequest("GET", "/sse", nil)
		req.Header.Set("Authorization", "Bearer "+expiredToken)
		w := httptest.NewRecorder()

		secured.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized for expired token, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("refresh token presented as access token is rejected", func(t *testing.T) {
		refreshToken := makeTestToken(jwt.SigningMethodHS256, jwtSigningKey, jwt.MapClaims{
			"sub":    "user",
			"type":   "refresh", // type mismatch for access endpoint
			"scopes": []string{"lexicon:read"},
			"exp":    time.Now().Add(10 * time.Minute).Unix(),
		})

		req := httptest.NewRequest("GET", "/sse", nil)
		req.Header.Set("Authorization", "Bearer "+refreshToken)
		w := httptest.NewRecorder()

		secured.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized for refresh token used as access token, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("token in query param fallback works", func(t *testing.T) {
		validToken := makeTestToken(jwt.SigningMethodHS256, jwtSigningKey, jwt.MapClaims{
			"sub":    "query-param-user",
			"type":   "access",
			"scopes": []string{"lexicon:read"},
			"exp":    time.Now().Add(10 * time.Minute).Unix(),
		})

		req := httptest.NewRequest("GET", "/sse?token="+validToken, nil)
		w := httptest.NewRecorder()

		secured.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for token in query parameter, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestOAuthMiddleware_TokenVersionRevocation(t *testing.T) {
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	secured := oauthMiddleware(innerHandler)

	t.Run("token with matching token_version succeeds", func(t *testing.T) {
		userCacheMu.Lock()
		userCache["user-v1"] = userCacheEntry{
			active:       true,
			tokenVersion: 1,
			expiresAt:    time.Now().Add(1 * time.Hour),
		}
		userCacheMu.Unlock()

		token := makeTestToken(jwt.SigningMethodHS256, jwtSigningKey, jwt.MapClaims{
			"sub":    "user-v1",
			"type":   "access",
			"tv":     1,
			"scopes": []string{"lexicon:read"},
			"exp":    time.Now().Add(10 * time.Minute).Unix(),
		})

		req := httptest.NewRequest("GET", "/sse", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()

		secured.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for matching token_version, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("missing tv claim is treated as version 0 (backward-compatible)", func(t *testing.T) {
		userCacheMu.Lock()
		userCache["legacy-user"] = userCacheEntry{
			active:       true,
			tokenVersion: 0,
			expiresAt:    time.Now().Add(1 * time.Hour),
		}
		userCacheMu.Unlock()

		legacyToken := makeTestToken(jwt.SigningMethodHS256, jwtSigningKey, jwt.MapClaims{
			"sub":    "legacy-user",
			"type":   "access",
			// No 'tv' claim
			"scopes": []string{"lexicon:read"},
			"exp":    time.Now().Add(10 * time.Minute).Unix(),
		})

		req := httptest.NewRequest("GET", "/sse", nil)
		req.Header.Set("Authorization", "Bearer "+legacyToken)
		w := httptest.NewRecorder()

		secured.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for legacy token without tv claim, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("revoked token (version mismatch) is rejected with 401", func(t *testing.T) {
		userCacheMu.Lock()
		// User has been revoked / bumped to version 2
		userCache["revoked-user"] = userCacheEntry{
			active:       true,
			tokenVersion: 2,
			expiresAt:    time.Now().Add(1 * time.Hour),
		}
		userCacheMu.Unlock()

		oldToken := makeTestToken(jwt.SigningMethodHS256, jwtSigningKey, jwt.MapClaims{
			"sub":    "revoked-user",
			"type":   "access",
			"tv":     1, // Stale version 1 < current version 2
			"scopes": []string{"lexicon:read"},
			"exp":    time.Now().Add(10 * time.Minute).Unix(),
		})

		req := httptest.NewRequest("GET", "/sse", nil)
		req.Header.Set("Authorization", "Bearer "+oldToken)
		w := httptest.NewRecorder()

		secured.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized for revoked token version, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "revoked") {
			t.Errorf("expected revocation error description in response, got: %s", w.Body.String())
		}
	})

	t.Run("deactivated user is rejected with 401", func(t *testing.T) {
		userCacheMu.Lock()
		userCache["deactivated-user"] = userCacheEntry{
			active:       false,
			tokenVersion: 0,
			expiresAt:    time.Now().Add(1 * time.Hour),
		}
		userCacheMu.Unlock()

		token := makeTestToken(jwt.SigningMethodHS256, jwtSigningKey, jwt.MapClaims{
			"sub":    "deactivated-user",
			"type":   "access",
			"tv":     0,
			"scopes": []string{"lexicon:read"},
			"exp":    time.Now().Add(10 * time.Minute).Unix(),
		})

		req := httptest.NewRequest("GET", "/sse", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()

		secured.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized for deactivated user, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "inactive or revoked") {
			t.Errorf("expected inactive error description in response, got: %s", w.Body.String())
		}
	})
}
