package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBuildClientRegistration exercises the pure RFC 7591 validation/build logic
// without touching Firestore.
func TestBuildClientRegistration(t *testing.T) {
	t.Run("valid public client", func(t *testing.T) {
		reg, err := buildClientRegistration(&ClientRegistrationRequest{
			RedirectURIs: []string{"https://spark.example/callback", "http://127.0.0.1:8080/callback"},
			ClientName:   "Spark",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.HasPrefix(reg.ClientID, dynamicClientIDPrefix) {
			t.Errorf("expected client_id to carry prefix %q, got %q", dynamicClientIDPrefix, reg.ClientID)
		}
		if isURLClientID(reg.ClientID) {
			t.Errorf("DCR client_id must not look like a CIMD URL: %q", reg.ClientID)
		}
		if reg.TokenEndpointAuthMethod != "none" {
			t.Errorf("expected public client (auth method 'none'), got %q", reg.TokenEndpointAuthMethod)
		}
		if reg.ClientIDIssuedAt == 0 {
			t.Errorf("expected client_id_issued_at to be set")
		}
		// Defaults applied.
		if len(reg.GrantTypes) == 0 || len(reg.ResponseTypes) == 0 {
			t.Errorf("expected default grant/response types, got %+v / %+v", reg.GrantTypes, reg.ResponseTypes)
		}
	})

	t.Run("missing redirect_uris rejected", func(t *testing.T) {
		if _, err := buildClientRegistration(&ClientRegistrationRequest{}); err == nil {
			t.Fatal("expected error for missing redirect_uris")
		}
	})

	t.Run("non-loopback http redirect rejected", func(t *testing.T) {
		if _, err := buildClientRegistration(&ClientRegistrationRequest{
			RedirectURIs: []string{"http://evil.example/callback"},
		}); err == nil {
			t.Fatal("expected error for non-loopback http redirect_uri")
		}
	})

	t.Run("too many redirect_uris rejected", func(t *testing.T) {
		uris := make([]string, maxRedirectURIs+1)
		for i := range uris {
			uris[i] = "https://spark.example/callback"
		}
		if _, err := buildClientRegistration(&ClientRegistrationRequest{RedirectURIs: uris}); err == nil {
			t.Fatal("expected error for too many redirect_uris")
		}
	})

	t.Run("confidential auth method rejected", func(t *testing.T) {
		if _, err := buildClientRegistration(&ClientRegistrationRequest{
			RedirectURIs:            []string{"https://spark.example/callback"},
			TokenEndpointAuthMethod: "client_secret_basic",
		}); err == nil {
			t.Fatal("expected error for non-public token_endpoint_auth_method")
		}
	})
}

// TestHandleClientRegistration exercises the HTTP handler. firestoreClient is nil
// under test, so the handler skips persistence and still returns a registration.
func TestHandleClientRegistration(t *testing.T) {
	t.Run("valid registration returns 201 and no secret", func(t *testing.T) {
		body := `{"redirect_uris":["https://spark.example/callback"],"client_name":"Spark"}`
		req := httptest.NewRequest("POST", "/api/oauth/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handleClientRegistration(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d (body: %s)", w.Code, w.Body.String())
		}
		var resp map[string]any
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if _, hasSecret := resp["client_secret"]; hasSecret {
			t.Errorf("public client response must not contain client_secret")
		}
		cid, _ := resp["client_id"].(string)
		if !strings.HasPrefix(cid, dynamicClientIDPrefix) {
			t.Errorf("expected issued client_id with prefix %q, got %q", dynamicClientIDPrefix, cid)
		}
	})

	t.Run("missing redirect_uris returns 400", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/oauth/register", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handleClientRegistration(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})

	t.Run("non-POST returns 405", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/oauth/register", nil)
		w := httptest.NewRecorder()
		handleClientRegistration(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", w.Code)
		}
	})
}

// TestClientIDDispatch verifies resolveClient routes by client_id shape: URL
// client_ids go to CIMD, opaque ones to the registered-client store.
func TestClientIDDispatch(t *testing.T) {
	cases := map[string]bool{
		"https://www.mithlond.com/metadata.json": true,
		"http://localhost:8080/metadata.json":    true,
		"mcp-client-abc123":                      false,
		"opaque-id":                              false,
	}
	for id, wantURL := range cases {
		if got := isURLClientID(id); got != wantURL {
			t.Errorf("isURLClientID(%q) = %v, want %v", id, got, wantURL)
		}
	}

	// With firestoreClient nil, an opaque client_id routes to the registered
	// store and surfaces a store-unavailable error (proving dispatch, not CIMD).
	if _, err := resolveClient(context.Background(), "mcp-client-does-not-exist"); err == nil {
		t.Error("expected error resolving unknown DCR client_id with no store")
	}
}
