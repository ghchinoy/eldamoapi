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

	// Create MCP server
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "test-eldamo-mcp-server",
		Version: "1.0.0",
	}, nil)

	// Register tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "enquire_lexicon",
		Description: "Search the Eldamo Tolkien lexicon.",
	}, enquireLexiconHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_word_details",
		Description: "Fetch complete details for a specific Eldamo entry.",
	}, getWordDetailsHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_derivations",
		Description: "Retrieve derivation history.",
	}, getDerivationsHandler)

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

	// 3. Test 'enquire_lexicon' tool
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

	// Test non-GET request
	reqPost := httptest.NewRequest("POST", "/.well-known/oauth-authorization-server", nil)
	wPost := httptest.NewRecorder()
	handleOAuthDiscovery(wPost, reqPost)
	if wPost.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected POST to return 405 Method Not Allowed, got %d", wPost.Code)
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
	userUID := "test-user-uid"
	clientID := "https://client.com/metadata.json"
	issuer := "https://www.mithlond.com"

	// 1. Generate access token
	tokenStr, err := generateJWT(userUID, clientID, issuer, 1*time.Hour, "access")
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
	validToken, err := generateJWT("user-123", "https://client.com/metadata.json", "https://www.mithlond.com", 1*time.Hour, "access")
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

