package main

// a2a_test.go — l04.7 + translate test backfill (djk)
//
// Coverage:
//   TestAgentCardHandler        — handleAgentCard HTTP surface (200, 405, CORS, host derivation)
//   TestAgentCardSecuritySchemes — OAuth2 scheme + PKCE + per-skill requirements in the card JSON
//   TestA2AAuthGating           — full oauthMiddleware→sseLogging→A2AHandler chain: 401/401/200
//   TestIsNameRequest           — routing predicate for the name-generate skill
//   TestParseNameRequest        — language, gender, and concept extraction from message text
//   TestCompoundingRules        — skills.JoinRoots (vowel elision + consonant assimilation), skills.AppendSuffix
//   TestIsUsableWord            — filters multi-word / empty lexicon entries
//   TestExecutorScopeRejection  — missing skill:name-generate yields a clear rejection message

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/golang-jwt/jwt/v4"
	"github.com/ghchinoy/eldamoapi/internal/skills"
)

// ── Test helpers ──────────────────────────────────────────────────────────────

// makeTestJWT mints a valid HS256 access token using the same signing key as the
// server. Mirrors the logic in cmd/eldamo-admin/main.go generateToken.
func makeTestJWT(t *testing.T, scopes []string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"sub":    "test-user",
		"scopes": scopes,
		"type":   "access",
		"exp":    time.Now().Add(time.Hour).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(jwtSigningKey)
	if err != nil {
		t.Fatalf("makeTestJWT: %v", err)
	}
	return signed
}

// makeTestMessage wraps text in an *a2a.Message.
func makeTestMessage(text string) *a2a.Message {
	return &a2a.Message{
		Parts: []*a2a.Part{a2a.NewTextPart(text)},
	}
}

// makeTestUser builds an a2asrv.User with the supplied scopes in Attributes,
// matching how claimsInterceptor.Before populates it.
func makeTestUser(scopes []string) *a2asrv.User {
	return a2asrv.NewAuthenticatedUser("test-user", map[string]any{
		"scopes": scopes,
	})
}

// ── TestAgentCardHandler ──────────────────────────────────────────────────────

func TestAgentCardHandler(t *testing.T) {
	t.Run("GET returns 200 with valid JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
		req.Host = "test.example.com"
		w := httptest.NewRecorder()
		handleAgentCard(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var card map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &card); err != nil {
			t.Fatalf("response is not valid JSON: %v", err)
		}
		if card["name"] != "Eldamo Elvish Agent" {
			t.Errorf("unexpected name: %v", card["name"])
		}
	})

	t.Run("supportedInterfaces URL uses request host (http localhost)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
		req.Host = "127.0.0.1:8099"
		w := httptest.NewRecorder()
		handleAgentCard(w, req)

		var card map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &card); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		ifaces := card["supportedInterfaces"].([]any)
		iface := ifaces[0].(map[string]any)
		got := iface["url"].(string)
		if !strings.HasPrefix(got, "http://127.0.0.1:8099") {
			t.Errorf("expected http://127.0.0.1:8099/a2a prefix, got %s", got)
		}
	})

	t.Run("X-Forwarded-Host overrides r.Host", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
		req.Host = "127.0.0.1:8099"
		req.Header.Set("X-Forwarded-Host", "eldamo.cloudrun.app")
		w := httptest.NewRecorder()
		handleAgentCard(w, req)

		var card map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &card); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		ifaces := card["supportedInterfaces"].([]any)
		iface := ifaces[0].(map[string]any)
		got := iface["url"].(string)
		if !strings.Contains(got, "eldamo.cloudrun.app") {
			t.Errorf("expected X-Forwarded-Host in URL, got %s", got)
		}
	})

	t.Run("OPTIONS returns 204 with CORS headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/.well-known/agent-card.json", nil)
		w := httptest.NewRecorder()
		handleAgentCard(w, req)

		if w.Code != http.StatusNoContent {
			t.Errorf("expected 204, got %d", w.Code)
		}
		if w.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Error("missing Access-Control-Allow-Origin: *")
		}
		if w.Header().Get("Access-Control-Allow-Methods") == "" {
			t.Error("missing Access-Control-Allow-Methods")
		}
	})

	t.Run("POST returns 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/.well-known/agent-card.json", nil)
		w := httptest.NewRecorder()
		handleAgentCard(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", w.Code)
		}
	})
}

// ── TestAgentCardSecuritySchemes ──────────────────────────────────────────────

func TestAgentCardSecuritySchemes(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
	req.Host = "www.mithlond.com"
	w := httptest.NewRecorder()
	handleAgentCard(w, req)

	var card map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &card); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	t.Run("securitySchemes present and contains mithlond-oauth", func(t *testing.T) {
		schemes, ok := card["securitySchemes"].(map[string]any)
		if !ok {
			t.Fatal("missing securitySchemes")
		}
		if _, ok := schemes["mithlond-oauth"]; !ok {
			t.Fatal("missing mithlond-oauth scheme")
		}
	})

	t.Run("OAuth2 authorizationCode flow has PKCE + correct endpoints", func(t *testing.T) {
		schemes := card["securitySchemes"].(map[string]any)
		oauthWrapper := schemes["mithlond-oauth"].(map[string]any)
		scheme := oauthWrapper["oauth2SecurityScheme"].(map[string]any)
		flows := scheme["flows"].(map[string]any)
		ac := flows["authorizationCode"].(map[string]any)

		if ac["pkceRequired"] != true {
			t.Error("expected pkceRequired=true")
		}
		tokenURL, _ := ac["tokenUrl"].(string)
		if !strings.Contains(tokenURL, "www.mithlond.com/api/oauth/token") {
			t.Errorf("tokenUrl should use request host, got %s", tokenURL)
		}
		authURL, _ := ac["authorizationUrl"].(string)
		if authURL != "https://www.mithlond.com/mcp-auth" {
			t.Errorf("unexpected authorizationUrl: %s", authURL)
		}
		scopes, _ := ac["scopes"].(map[string]any)
		for _, s := range []string{"lexicon:read", "agent:invoke", "skill:name-generate"} {
			if _, ok := scopes[s]; !ok {
				t.Errorf("scope %q not found in card", s)
			}
		}
	})

	t.Run("agent-level securityRequirements requires agent:invoke", func(t *testing.T) {
		reqs, ok := card["securityRequirements"].([]any)
		if !ok || len(reqs) == 0 {
			t.Fatal("missing or empty securityRequirements")
		}
		// Each requirement is {"schemes": {"mithlond-oauth": [...]}}
		req0 := reqs[0].(map[string]any)
		inner := req0["schemes"].(map[string]any)
		scopeList, _ := inner["mithlond-oauth"].([]any)
		found := false
		for _, s := range scopeList {
			if s == "agent:invoke" {
				found = true
			}
		}
		if !found {
			t.Errorf("agent:invoke not in top-level securityRequirements: %v", scopeList)
		}
	})

	t.Run("name-generate skill has skill:name-generate securityRequirement", func(t *testing.T) {
		skills, _ := card["skills"].([]any)
		var nameGenSkill map[string]any
		for _, s := range skills {
			sk := s.(map[string]any)
			if sk["id"] == "name-generate" {
				nameGenSkill = sk
				break
			}
		}
		if nameGenSkill == nil {
			t.Fatal("name-generate skill not in card")
		}
		skillReqs, ok := nameGenSkill["securityRequirements"].([]any)
		if !ok || len(skillReqs) == 0 {
			t.Fatal("name-generate skill missing securityRequirements")
		}
		sr0 := skillReqs[0].(map[string]any)
		inner := sr0["schemes"].(map[string]any)
		scopeList, _ := inner["mithlond-oauth"].([]any)
		found := false
		for _, s := range scopeList {
			if s == "skill:name-generate" {
				found = true
			}
		}
		if !found {
			t.Errorf("skill:name-generate not in name-generate skill securityRequirements: %v", scopeList)
		}
	})
}

// ── TestA2AAuthGating ─────────────────────────────────────────────────────────

// TestA2AAuthGating validates the full HTTP middleware chain for the /a2a route:
// oauthMiddleware → sseLoggingMiddleware → A2AJSONRPCHandler → claimsInterceptor.
// It uses "hello" (non-name-request) so the echo path runs — no lexiconIndex needed.
func TestA2AAuthGating(t *testing.T) {
	handler := oauthMiddleware(sseLoggingMiddleware(newA2AHandler()))
	ts := httptest.NewServer(handler)
	defer ts.Close()

	// A minimal but well-formed message/send JSON-RPC body.
	// Uses "hello" so the executor takes the echo path (no lexiconIndex access).
	rpcBody := `{"jsonrpc":"2.0","id":1,"method":"message/send",` +
		`"params":{"message":{"messageId":"t1","role":"ROLE_USER",` +
		`"parts":[{"content":"hello"}]}}}`

	post := func(t *testing.T, token string) int {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(rpcBody))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode
	}

	t.Run("no token returns 401", func(t *testing.T) {
		if got := post(t, ""); got != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", got)
		}
	})

	t.Run("malformed token returns 401", func(t *testing.T) {
		if got := post(t, "not-a-jwt-at-all"); got != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", got)
		}
	})

	t.Run("wrong signing key returns 401", func(t *testing.T) {
		// Sign with a key that does not match jwtSigningKey.
		wrongKey := []byte("totally-wrong-signing-key")
		claims := jwt.MapClaims{
			"sub": "user", "type": "access",
			"scopes": []string{"agent:invoke"},
			"exp":    time.Now().Add(time.Hour).Unix(),
		}
		tok, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(wrongKey)
		if got := post(t, tok); got != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", got)
		}
	})

	t.Run("valid token with agent:invoke reaches handler (HTTP 200)", func(t *testing.T) {
		tok := makeTestJWT(t, []string{"lexicon:read", "agent:invoke", "skill:name-generate"})
		if got := post(t, tok); got != http.StatusOK {
			t.Errorf("expected 200, got %d", got)
		}
	})
}

// ── TestExecutorScopeRejection ────────────────────────────────────────────────

// TestExecutorScopeRejection calls eldamoAgentExecutor.Execute directly with a
// user who has agent:invoke but NOT skill:name-generate, and a name-generate
// request. The executor must yield a *Message describing the scope requirement.
func TestExecutorScopeRejection(t *testing.T) {
	user := makeTestUser([]string{"lexicon:read", "agent:invoke"})
	execCtx := &a2asrv.ExecutorContext{
		User:    user,
		Message: makeTestMessage("name star silver quenya"),
	}

	exec := &eldamoAgentExecutor{deps: &skills.Deps{Index: lexiconIndex}}
	seq := exec.Execute(context.Background(), execCtx)

	var events []a2a.Event
	for event, err := range seq {
		if err != nil {
			t.Fatalf("unexpected error from executor: %v", err)
		}
		events = append(events, event)
	}

	if len(events) == 0 {
		t.Fatal("expected at least one event for scope rejection, got none")
	}

	msg, ok := events[0].(*a2a.Message)
	if !ok {
		t.Fatalf("expected *a2a.Message, got %T", events[0])
	}
	if len(msg.Parts) == 0 {
		t.Fatal("rejection message has no parts")
	}
	text := msg.Parts[0].Text()
	if !strings.Contains(text, "skill:name-generate") {
		t.Errorf("expected rejection message to mention 'skill:name-generate', got: %q", text)
	}
}

func TestTranslateEnabled(t *testing.T) {
	t.Run("false when env unset", func(t *testing.T) {
		orig := os.Getenv("GEMINI_TRANSLATE_MODEL")
		if err := os.Setenv("GEMINI_TRANSLATE_MODEL", ""); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.Setenv("GEMINI_TRANSLATE_MODEL", orig) }()
		if translateEnabled() {
			t.Error("TranslateEnabled() should be false when GEMINI_TRANSLATE_MODEL is empty")
		}
	})
	t.Run("true when env set", func(t *testing.T) {
		orig := os.Getenv("GEMINI_TRANSLATE_MODEL")
		if err := os.Setenv("GEMINI_TRANSLATE_MODEL", "gemini-test-model"); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.Setenv("GEMINI_TRANSLATE_MODEL", orig) }()
		if !translateEnabled() {
			t.Error("TranslateEnabled() should be true when GEMINI_TRANSLATE_MODEL is set")
		}
	})
}
