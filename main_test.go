package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ghchinoy/eldamoapi/data"
	"github.com/ghchinoy/eldamoapi/index"
	"github.com/golang-jwt/jwt/v4"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)
func TestIntegrationServer(t *testing.T) {
	rawBytes, err := data.GetJSONL()
	if err != nil {
		t.Fatalf("Failed to decompress embedded dataset: %v", err)
	}
	lexiconIndex, err = index.NewIndex(rawBytes)
	if err != nil {
		t.Fatalf("Failed to initialize search index: %v", err)
	}

	// Create MCP server via shared initializer
	server := createMCPServer()

	// Setup multiplexed handler
	handler := NewMcpMultiplexerHandler(func(*http.Request) *mcp.Server { return server })

	// Wrap handler with logging/SSE headers middleware
	sseHandler := sseLoggingMiddleware(handler)

	// Spin up test server
	testMux := http.NewServeMux()
	testMux.Handle("/sse", sseHandler)
	ts := httptest.NewServer(testMux)
	defer ts.Close()

	ctx := context.Background()

	// Connect client to server directly (unprotected during OAuth transition)
	transport := &mcp.SSEClientTransport{
		Endpoint: ts.URL + "/sse",
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil)
	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer func() {
		_ = cs.Close()
	}()

	// 1. Test ListTools metadata (Titles, Annotations, OutputSchema)
	toolsResult, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to list tools: %v", err)
	}
	if len(toolsResult.Tools) < 4 {
		t.Errorf("Expected at least 4 tools, got %d", len(toolsResult.Tools))
	}
	for _, tool := range toolsResult.Tools {
		if tool.Title == "" {
			t.Errorf("Tool '%s' missing Title", tool.Name)
		}
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("Tool '%s' expected ReadOnlyHint = true", tool.Name)
		}
		if tool.Name != "render_elvish_audio" && tool.OutputSchema == nil {
			t.Errorf("Read tool '%s' expected non-nil OutputSchema", tool.Name)
		}
	}

	// 2. Test 'enquire_lexicon' tool (dual-emit: text + structured content)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "enquire_lexicon",
		Arguments: map[string]any{"query": "star"},
	})
	if err != nil {
		t.Fatalf("Failed to call enquire_lexicon: %v", err)
	}
	if len(res.Content) == 0 {
		t.Fatal("Expected response content, got empty")
	}
	textResult := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(textResult, "Found") {
		t.Errorf("Expected response text to contain 'Found', got: %s", textResult)
	}
	if res.StructuredContent == nil {
		t.Error("Expected dual-emit StructuredContent, got nil")
	}

	// 3. Test Prompts (ListPrompts & GetPrompt)
	promptsResult, err := cs.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to list prompts: %v", err)
	}
	if len(promptsResult.Prompts) != 3 {
		t.Errorf("Expected 3 prompts, got %d", len(promptsResult.Prompts))
	}
	getPromptRes, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{
		Name:      "tolkien-translation",
		Arguments: map[string]string{"text": "namarie"},
	})
	if err != nil {
		t.Fatalf("Failed to get prompt 'tolkien-translation': %v", err)
	}
	if len(getPromptRes.Messages) == 0 {
		t.Fatal("Expected prompt message content, got empty")
	}
	promptText := getPromptRes.Messages[0].Content.(*mcp.TextContent).Text
	if !strings.Contains(promptText, "Text to translate: namarie") {
		t.Errorf("Expected prompt text to contain input text, got: %s", promptText)
	}

	// 4. Test Resources (ListResources & ReadResource)
	resourcesResult, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to list resources: %v", err)
	}
	if len(resourcesResult.Resources) != 2 {
		t.Errorf("Expected 2 resources, got %d", len(resourcesResult.Resources))
	}
	readCardRes, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "eldamo://agent-card"})
	if err != nil {
		t.Fatalf("Failed to read resource 'eldamo://agent-card': %v", err)
	}
	if len(readCardRes.Contents) == 0 || readCardRes.Contents[0].Text == "" {
		t.Error("Expected non-empty resource content for agent-card")
	}

	// 4. Test 'enquire_lexicon' with no results
	resNoMatches, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "enquire_lexicon",
		Arguments: map[string]any{"query": "non-existent-gobbldygook-word"},
	})
	if err != nil {
		t.Fatalf("Failed to call enquire_lexicon with non-matching query: %v", err)
	}
	textNoMatches := resNoMatches.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(textNoMatches, "No matches found") {
		t.Errorf("Expected response text to contain 'No matches found', got: %s", textNoMatches)
	}

	// 5. Test 'get_word_details' with a dummy ID (or we can extract a real ID from textResult)
	var testWord *index.FlatWord
	for _, w := range lexiconIndex.Words {
		if w.Word != "" && len(w.Refs) > 0 {
			testWord = w
			break
		}
	}
	if testWord == nil {
		t.Skip("No words with references found in index for testing detail/derivations")
		return
	}

	resDetails, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_word_details",
		Arguments: map[string]any{"id": testWord.ID},
	})
	if err != nil {
		t.Fatalf("Failed to call get_word_details: %v", err)
	}
	textDetails := resDetails.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(textDetails, testWord.Word) {
		t.Errorf("Expected word details to contain word '%s', got: %s", testWord.Word, textDetails)
	}

	// 6. Test 'get_derivations'
	resDerivs, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_derivations",
		Arguments: map[string]any{"id": testWord.ID, "direction": "ancestors"},
	})
	if err != nil {
		t.Fatalf("Failed to call get_derivations: %v", err)
	}
	textDerivs := resDerivs.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(textDerivs, "ancestors") && !strings.Contains(textDerivs, "No ancestors found") {
		t.Errorf("Unexpected get_derivations output: %s", textDerivs)
	}
}

func TestMcpMultiplexerRouting(t *testing.T) {
	sseCalled := false
	streamableCalled := false

	dummySSE := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sseCalled = true
		w.WriteHeader(http.StatusOK)
	})
	dummyStreamable := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		streamableCalled = true
		w.WriteHeader(http.StatusOK)
	})

	mux := &McpMultiplexerHandler{
		sseHandler:        dummySSE,
		streamableHandler: dummyStreamable,
	}

	resetCalls := func() {
		sseCalled = false
		streamableCalled = false
	}

	// Case 1: Standard SSE GET request
	resetCalls()
	req := httptest.NewRequest("GET", "/sse", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if !sseCalled || streamableCalled {
		t.Errorf("Expected GET /sse to route to SSEHandler")
	}

	// Case 2: Standard SSE POST request with sessionid
	resetCalls()
	req = httptest.NewRequest("POST", "/sse?sessionid=123", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if !sseCalled || streamableCalled {
		t.Errorf("Expected POST /sse?sessionid=123 to route to SSEHandler")
	}

	// Case 2b: Standard SSE POST request with camelCase sessionId
	resetCalls()
	req = httptest.NewRequest("POST", "/sse?sessionId=123", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if !sseCalled || streamableCalled {
		t.Errorf("Expected POST /sse?sessionId=123 to route to SSEHandler")
	}

	// Case 3: Streamable HTTP handshake POST without sessionid
	resetCalls()
	req = httptest.NewRequest("POST", "/sse", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if sseCalled || !streamableCalled {
		t.Errorf("Expected POST /sse (no query) to route to StreamableHTTPHandler")
	}

	// Case 4: Streamable HTTP DELETE request
	resetCalls()
	req = httptest.NewRequest("DELETE", "/sse", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if sseCalled || !streamableCalled {
		t.Errorf("Expected DELETE /sse to route to StreamableHTTPHandler")
	}

	// Case 5: Streamable HTTP request with Mcp-Session-Id header
	resetCalls()
	req = httptest.NewRequest("POST", "/sse", nil)
	req.Header.Set("Mcp-Session-Id", "abc-xyz")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if sseCalled || !streamableCalled {
		t.Errorf("Expected request with Mcp-Session-Id header to route to StreamableHTTPHandler")
	}

	// Case 6: X-Mcp-Force-Sse on GET routes to SSEHandler (the header's intended use:
	// forcing the long-lived streaming connection off Cloud Run/GFE buffering).
	resetCalls()
	req = httptest.NewRequest("GET", "/sse", nil)
	req.Header.Set("X-Mcp-Force-Sse", "true")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if !sseCalled || streamableCalled {
		t.Errorf("Expected GET /sse with X-Mcp-Force-Sse to route to SSEHandler")
	}

	// Case 7: X-Mcp-Force-Sse on a bare POST (Streamable-HTTP "initialize" handshake,
	// no prior GET/session) must NOT be forced onto SSEHandler — the legacy SSE
	// handler requires a session established by a prior GET and would 400 this
	// request. Regression test for the "sending initialize: Bad Request" bug hit by
	// Streamable-HTTP-only clients (e.g. Antigravity CLI/agy) that attach this header
	// unconditionally to every request.
	resetCalls()
	req = httptest.NewRequest("POST", "/sse", nil)
	req.Header.Set("X-Mcp-Force-Sse", "true")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if sseCalled || !streamableCalled {
		t.Errorf("Expected POST /sse with X-Mcp-Force-Sse (no session) to route to StreamableHTTPHandler")
	}

	// Case 8: X-Mcp-Force-Sse on DELETE must also fall through to StreamableHTTPHandler.
	resetCalls()
	req = httptest.NewRequest("DELETE", "/sse", nil)
	req.Header.Set("X-Mcp-Force-Sse", "true")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if sseCalled || !streamableCalled {
		t.Errorf("Expected DELETE /sse with X-Mcp-Force-Sse to route to StreamableHTTPHandler")
	}

	// Case 9: X-Mcp-Force-Sse on a legacy-SSE POST (with sessionid) should still work
	// exactly as an unadorned legacy request would (SSEHandler), i.e. the header is a
	// no-op here rather than a behavior change.
	resetCalls()
	req = httptest.NewRequest("POST", "/sse?sessionid=123", nil)
	req.Header.Set("X-Mcp-Force-Sse", "true")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if !sseCalled || streamableCalled {
		t.Errorf("Expected POST /sse?sessionid=123 with X-Mcp-Force-Sse to route to SSEHandler")
	}
}

// TestRootPathMountedForBaseURLProbes verifies the MCP transport is reachable at
// the exact base path (so clients like Gemini Spark that probe POST / and HEAD /
// get an auth challenge instead of a 404), while the "/{$}" pattern keeps
// unknown paths returning 404.
func TestRootPathMountedForBaseURLProbes(t *testing.T) {
	t.Setenv("AUTH_BYPASS", "")

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK) // only reached when authenticated
	})
	secured := oauthMiddleware(inner)

	mux := http.NewServeMux()
	mux.Handle("/{$}", secured)
	mux.Handle("/sse", secured)

	// Base-URL probes reach the auth-gated transport -> 401, not a bare 404.
	for _, m := range []string{"POST", "HEAD", "GET"} {
		req := httptest.NewRequest(m, "/", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Errorf("%s / returned 404; expected transport mounted at root to issue an auth challenge", m)
		}
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s / expected 401 (unauthenticated transport), got %d", m, w.Code)
		}
	}

	// Unknown paths still 404 — the root pattern is exact-match only.
	req := httptest.NewRequest("GET", "/favicon.ico", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown path /favicon.ico, got %d", w.Code)
	}
}

func TestOAuthDiscovery(t *testing.T) {
	// Test standard GET request to discovery endpoint
	req := httptest.NewRequest("GET", "/.well-known/oauth-authorization-server", nil)
	req.Host = "www.mithlond.com"
	w := httptest.NewRecorder()

	handleOAuthDiscovery(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got '%s'", contentType)
	}

	// Verify JSON structure and dynamic Host inclusion
	var resp map[string]any
	var err error
	
	// Check decoding
	importDecoder := json.NewDecoder(w.Body)
	if err = importDecoder.Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	expectedIssuer := "https://www.mithlond.com"
	if resp["issuer"] != expectedIssuer {
		t.Errorf("Expected issuer '%s', got '%s'", expectedIssuer, resp["issuer"])
	}

	expectedAuthEndpoint := "https://www.mithlond.com/mcp-auth"
	if resp["authorization_endpoint"] != expectedAuthEndpoint {
		t.Errorf("Expected authorization_endpoint '%s', got '%s'", expectedAuthEndpoint, resp["authorization_endpoint"])
	}

	expectedTokenEndpoint := "https://www.mithlond.com/api/oauth/token"
	if resp["token_endpoint"] != expectedTokenEndpoint {
		t.Errorf("Expected token_endpoint '%s', got '%s'", expectedTokenEndpoint, resp["token_endpoint"])
	}

	// RFC 7591 DCR must be advertised alongside CIMD so DCR-only clients (Spark)
	// can self-register while CIMD clients keep working.
	expectedRegEndpoint := "https://www.mithlond.com/api/oauth/register"
	if resp["registration_endpoint"] != expectedRegEndpoint {
		t.Errorf("Expected registration_endpoint '%s', got '%s'", expectedRegEndpoint, resp["registration_endpoint"])
	}
	if resp["client_id_metadata_document_supported"] != true {
		t.Errorf("Expected CIMD support to remain advertised alongside DCR")
	}

	// Test non-GET request
	reqPost := httptest.NewRequest("POST", "/.well-known/oauth-authorization-server", nil)
	wPost := httptest.NewRecorder()
	handleOAuthDiscovery(wPost, reqPost)
	if wPost.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected POST to return 405 Method Not Allowed, got %d", wPost.Code)
	}
}

func TestProtectedResourceMetadata(t *testing.T) {
	decode := func(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
		t.Helper()
		if w.Code != http.StatusOK {
			t.Fatalf("Expected status code 200, got %d", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("Expected Content-Type 'application/json', got '%s'", ct)
		}
		var resp map[string]any
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		return resp
	}

	// 1. Bare path: resource identifier is the base URL, and the authorization
	//    server points back to this host's RFC 8414 document.
	req := httptest.NewRequest("GET", "/.well-known/oauth-protected-resource", nil)
	req.Host = "candir.mithlond.com"
	w := httptest.NewRecorder()
	handleProtectedResourceMetadata(w, req)
	resp := decode(t, w)

	if resp["resource"] != "https://candir.mithlond.com" {
		t.Errorf("Expected resource 'https://candir.mithlond.com', got '%v'", resp["resource"])
	}
	authServers, ok := resp["authorization_servers"].([]any)
	if !ok || len(authServers) != 1 || authServers[0] != "https://candir.mithlond.com" {
		t.Errorf("Expected authorization_servers ['https://candir.mithlond.com'], got '%v'", resp["authorization_servers"])
	}

	// 2. Resource-path-suffixed variant (RFC 9728): resource reflects the /sse path.
	reqSSE := httptest.NewRequest("GET", "/.well-known/oauth-protected-resource/sse", nil)
	reqSSE.Host = "candir.mithlond.com"
	wSSE := httptest.NewRecorder()
	handleProtectedResourceMetadata(wSSE, reqSSE)
	respSSE := decode(t, wSSE)
	if respSSE["resource"] != "https://candir.mithlond.com/sse" {
		t.Errorf("Expected resource 'https://candir.mithlond.com/sse', got '%v'", respSSE["resource"])
	}

	// 3. Non-GET (other than OPTIONS) is rejected.
	reqPost := httptest.NewRequest("POST", "/.well-known/oauth-protected-resource", nil)
	wPost := httptest.NewRecorder()
	handleProtectedResourceMetadata(wPost, reqPost)
	if wPost.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected POST to return 405 Method Not Allowed, got %d", wPost.Code)
	}

	// 4. Preflight OPTIONS returns CORS headers and 204.
	reqOpt := httptest.NewRequest("OPTIONS", "/.well-known/oauth-protected-resource", nil)
	wOpt := httptest.NewRecorder()
	handleProtectedResourceMetadata(wOpt, reqOpt)
	if wOpt.Code != http.StatusNoContent {
		t.Errorf("Expected OPTIONS to return 204, got %d", wOpt.Code)
	}
	if wOpt.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("Expected permissive CORS on OPTIONS preflight")
	}
}

// TestUnauthorizedChallengePointsToPRM verifies the 401 WWW-Authenticate header
// carries the RFC 9728 resource_metadata pointer so clients can discover the
// Protected Resource Metadata document.
func TestUnauthorizedChallengePointsToPRM(t *testing.T) {
	t.Setenv("AUTH_BYPASS", "")

	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest("GET", "/sse", nil)
	req.Host = "candir.mithlond.com"
	w := httptest.NewRecorder()

	oauthMiddleware(dummy).ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401, got %d", w.Code)
	}
	challenge := w.Header().Get("WWW-Authenticate")
	want := `resource_metadata="https://candir.mithlond.com/.well-known/oauth-protected-resource"`
	if !strings.Contains(challenge, want) {
		t.Errorf("Expected WWW-Authenticate to contain %q, got %q", want, challenge)
	}
}

func TestSSRFBlocking(t *testing.T) {
	client := SafeHTTPClient()

	// 1. Try to fetch a loopback address (IsLoopback is blocked)
	_, err := client.Get("http://127.0.0.1:1234/internal")
	if err == nil {
		t.Fatal("Expected local loopback request to be blocked by SSRF dialer")
	}
	if !strings.Contains(err.Error(), "connection to private/loopback IP blocked") {
		t.Errorf("Expected connection blocked error, got: %v", err)
	}

	// 2. Try to fetch a private IP address (IsPrivate is blocked)
	_, err = client.Get("http://192.168.1.1:80/status")
	if err == nil {
		t.Fatal("Expected private IP request to be blocked by SSRF dialer")
	}
}

func TestCIMDParsing(t *testing.T) {
	// Temporarily allow local IPs for mock server testing
	allowLocalIPs = true
	defer func() {
		allowLocalIPs = false
	}()

	// Start a local mock server that returns client metadata
	mockMeta := ClientIDMetadata{
		ClientID:     "http://localhost:9999/metadata.json", // IsLocal allows http
		ClientName:   "Test AI Client",
		RedirectURIs: []string{"http://localhost:8080/callback"},
	}

	// Dynamic handler to update ClientID to match dynamic port
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mockMeta.ClientID = "http://" + r.Host + "/metadata.json"
		_ = json.NewEncoder(w).Encode(mockMeta)
	}))
	defer ts.Close()

	// Fetch and validate
	meta, err := FetchAndValidateCIMD(context.Background(), ts.URL+"/metadata.json")
	if err != nil {
		t.Fatalf("Failed to fetch and validate CIMD: %v", err)
	}

	if meta.ClientName != "Test AI Client" {
		t.Errorf("Expected ClientName 'Test AI Client', got '%s'", meta.ClientName)
	}
}

func TestJWTGenerationAndValidation(t *testing.T) {
	testUser := &User{
		UID:    "test-user-uid",
		Scopes: []string{"lexicon:read"},
		Roles:  []string{"user"},
	}
	userUID := testUser.UID
	clientID := "https://client.com/metadata.json"
	issuer := "https://www.mithlond.com"

	// 1. Generate access token
	tokenStr, err := generateJWT(testUser, clientID, issuer, 1*time.Hour, "access")
	if err != nil {
		t.Fatalf("Failed to generate JWT: %v", err)
	}

	// 2. Parse and validate
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		return jwtSigningKey, nil
	})
	if err != nil {
		t.Fatalf("Failed to parse JWT: %v", err)
	}

	if !token.Valid {
		t.Fatal("Expected JWT to be valid")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatal("Failed to extract claims")
	}

	if claims["sub"] != userUID {
		t.Errorf("Expected sub '%s', got '%v'", userUID, claims["sub"])
	}
	if claims["client_id"] != clientID {
		t.Errorf("Expected client_id '%s', got '%v'", clientID, claims["client_id"])
	}
	if claims["type"] != "access" {
		t.Errorf("Expected type 'access', got '%v'", claims["type"])
	}
}

func TestOAuthMiddlewareSecurity(t *testing.T) {
	// Start a mock server wrapped in oauthMiddleware
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Access Granted"))
	})

	secured := oauthMiddleware(dummyHandler)
	ts := httptest.NewServer(secured)
	defer ts.Close()

	// 1. Test request without any token (should be blocked with 401 Unauthorized)
	reqNoToken, _ := http.NewRequest("GET", ts.URL, nil)
	respNoToken, err := http.DefaultClient.Do(reqNoToken)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if respNoToken.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected status 401 Unauthorized, got %d", respNoToken.StatusCode)
	}

	// 2. Test request with an invalid token (should be blocked with 401 Unauthorized)
	reqInvalidToken, _ := http.NewRequest("GET", ts.URL, nil)
	reqInvalidToken.Header.Set("Authorization", "Bearer invalid-gobbldygook-token")
	respInvalidToken, err := http.DefaultClient.Do(reqInvalidToken)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if respInvalidToken.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", respInvalidToken.StatusCode)
	}

	// 3. Test request with a VALID access token generated by our helper!
	testUser := &User{
		UID:    "user-123",
		Scopes: []string{"lexicon:read"},
		Roles:  []string{"user"},
	}
	validToken, err := generateJWT(testUser, "https://client.com/metadata.json", "https://www.mithlond.com", 1*time.Hour, "access")
	if err != nil {
		t.Fatalf("Failed to generate JWT: %v", err)
	}

	reqValidToken, _ := http.NewRequest("GET", ts.URL, nil)
	reqValidToken.Header.Set("Authorization", "Bearer "+validToken)
	respValidToken, err := http.DefaultClient.Do(reqValidToken)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if respValidToken.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 OK, got %d", respValidToken.StatusCode)
	}
}

