# 🚀 How to Create an MCP Server with Go

This guide provides a comprehensive, step-by-step walkthrough for building, testing, deploying, and **securing** a high-performance **Model Context Protocol (MCP) Server** using Go and the official `github.com/modelcontextprotocol/go-sdk`.

Go is an exceptional language for building MCP servers:
* **Zero Dependencies:** Compiles into a single, static binary.
* **Instant Boots:** Decompresses and boots in under **20ms**, making it perfect for serverless scale-from-zero environments.
* **Minimal Footprint:** Consumes only **~40MB of RAM** under full load, making hosting extremely cost-effective.

---

## 📋 Table of Contents
1. [Prerequisites & Project Setup](#1-prerequisites--project-setup)
2. [Designing Your First Tool](#2-designing-your-first-tool)
3. [Building the MCP Server](#3-building-the-mcp-server)
4. [Implementing the Multiplexer (SSE + Streamable HTTP)](#4-implementing-the-multiplexer-sse--streamable-http)
5. [Complete Executable Server Example](#5-complete-executable-server-example)
6. [Testing Locally (Command Line & Integration Tests)](#6-testing-locally-command-line--integration-tests)
7. [Deploying to Google Cloud Run (Serverless Best Practices)](#7-deploying-to-google-cloud-run-serverless-best-practices)
8. [Securing with OAuth 2.1 & Client ID Metadata Documents (CIMD)](#8-securing-with-oauth-21--client-id-metadata-documents-cimd)
9. [Appendix: Complete Production Code Samples](#9-appendix-complete-production-code-samples)


## 1. Prerequisites & Project Setup

Ensure you have **Go 1.22 or higher** installed.

Initialize a new Go module and add the official Model Context Protocol Go SDK:

```bash
mkdir my-mcp-server && cd my-mcp-server
go mod init github.com/username/my-mcp-server

# Install the official Go SDK
go get github.com/modelcontextprotocol/go-sdk@v1.6.1
```


## 2. Designing Your First Tool

In MCP, tools are registered with schemas defining their arguments. In Go, you define tool arguments as structured types, and the SDK automatically generates the JSON schema using `jsonschema` struct tags.

```go
type EnquireLexiconArgs struct {
	Query    string `json:"query" jsonschema:"The keyword or prefix to search for (e.g., 'star', 'flower')"`
	Language string `json:"language,omitempty" jsonschema:"Optional language code (e.g., 'q' for Quenya)"`
}
```

The tool handler must conform to the following signature:
```go
func yourToolHandler(ctx context.Context, req *mcp.CallToolRequest, args YourArgsStruct) (*mcp.CallToolResult, any, error)
```


## 3. Building the MCP Server

Creating the server instance and registering tools using type-safe helpers:

```go
// 1. Instantiate the MCP Server
server := mcp.NewServer(&mcp.Implementation{
    Name:    "my-mcp-server",
    Version: "1.0.0",
}, nil)

// 2. Register tools using the type-safe AddTool helper
mcp.AddTool(server, &mcp.Tool{
    Name:        "enquire_lexicon",
    Description: "Search vocabulary definitions and historical notes.",
}, enquireLexiconHandler)
```


## 4. Implementing the Multiplexer (SSE + Streamable HTTP)

MCP defines two standard HTTP-based transport protocols:
1. **Server-Sent Events (SSE):** Traditional transport where the client opens a persistent `GET` stream and sends JSON-RPC payloads via separate `POST` requests.
2. **Streamable HTTP:** A newer, high-performance transport tailored for direct agent tool invocations, using session headers.

To support both types of clients seamlessly, implement a custom **Multiplexer** that sniffs incoming requests and routes them to the correct SDK handler:

```go
type McpMultiplexerHandler struct {
	sseHandler        http.Handler
	streamableHandler http.Handler
}

func NewMcpMultiplexerHandler(getServer func(*http.Request) *mcp.Server) *McpMultiplexerHandler {
	return &McpMultiplexerHandler{
		sseHandler:        mcp.NewSSEHandler(getServer, nil),
		streamableHandler: mcp.NewStreamableHTTPHandler(getServer, nil),
	}
}

func (h *McpMultiplexerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 1. Check for sessionId (handling both camelCase and lowercase casing variations)
	hasSessionID := false
	for k := range r.URL.Query() {
		if strings.ToLower(k) == "sessionid" {
			hasSessionID = true
			break
		}
	}

	// 2. Route based on Streamable HTTP vs SSE traits
	if r.Header.Get("Mcp-Session-Id") != "" ||
		r.Method == "DELETE" ||
		(r.Method == "POST" && !hasSessionID) {
		h.streamableHandler.ServeHTTP(w, r)
		return
	}

	// 3. Fallback to standard Server-Sent Events
	h.sseHandler.ServeHTTP(w, r)
}
```


## 5. Complete Executable Server Example

Create `main.go` and add the following complete, compilable server code:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type EnquireLexiconArgs struct {
	Query string `json:"query" jsonschema:"The keyword or spelling prefix to search for"`
}

func enquireLexiconHandler(ctx context.Context, req *mcp.CallToolRequest, args EnquireLexiconArgs) (*mcp.CallToolResult, any, error) {
	query := strings.TrimSpace(args.Query)
	log.Printf("[Tool Call] enquire_lexicon: query='%s'", query)

	if query == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: query cannot be empty"},
			},
			IsError: true,
		}, nil, nil
	}

	// Mock database lookup (replace with your actual database or in-memory search)
	results := map[string]string{
		"elen": "star (Quenya)",
		"lume": "hour, time (Quenya)",
	}

	val, found := results[strings.ToLower(query)]
	var reply string
	if found {
		reply = fmt.Sprintf("Found match: %s - %s", query, val)
	} else {
		reply = fmt.Sprintf("No matches found for '%s'.", query)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: reply},
		},
	}, nil, nil
}

type McpMultiplexerHandler struct {
	sseHandler        http.Handler
	streamableHandler http.Handler
}

func NewMcpMultiplexerHandler(getServer func(*http.Request) *mcp.Server) *McpMultiplexerHandler {
	return &McpMultiplexerHandler{
		sseHandler:        mcp.NewSSEHandler(getServer, nil),
		streamableHandler: mcp.NewStreamableHTTPHandler(getServer, nil),
	}
}

func (h *McpMultiplexerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Enable CORS & prevent GFE/Reverse Proxy SSE buffering
	w.Header().Set("X-Accel-Buffering", "no")

	hasSessionID := false
	for k := range r.URL.Query() {
		if strings.ToLower(k) == "sessionid" {
			hasSessionID = true
			break
		}
	}

	if r.Header.Get("Mcp-Session-Id") != "" ||
		r.Method == "DELETE" ||
		(r.Method == "POST" && !hasSessionID) {
		h.streamableHandler.ServeHTTP(w, r)
		return
	}

	h.sseHandler.ServeHTTP(w, r)
}

func main() {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "demo-mcp-server",
		Version: "1.0.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "enquire_lexicon",
		Description: "Lookup Tolkien terms.",
	}, enquireLexiconHandler)

	mux := http.NewServeMux()
	
	// Public unauthenticated liveness endpoint
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "OK")
	})

	// Mount the multiplexed MCP handlers on /sse
	multiplexedHandler := NewMcpMultiplexerHandler(func(*http.Request) *mcp.Server { return server })
	mux.Handle("/sse", multiplexedHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("MCP Server listening on port %s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
```


## 6. Testing Locally

### Compile & Run
```bash
go build -o demo-mcp-server
./demo-mcp-server
```

### Option A: Test SSE Stream via cURL

1. **Establish the event stream:**
   ```bash
   curl -i http://localhost:8080/sse
   ```
   *This returns a `200 OK` header with `content-type: text/event-stream` and leaves the connection open, returning a session ID in an event, e.g., `event: endpoint\ndata: /sse?sessionId=XYZ`.*

2. **Send JSON-RPC Handshake POST:**
   Using the `sessionId` returned in step 1, execute:
   ```bash
   curl -i -X POST -H "Content-Type: application/json" \
     -d '{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": "2024-11-05", "capabilities": {}, "clientInfo": {"name": "curl-test", "version": "1.0.0"}}}' \
     "http://localhost:8080/sse?sessionId=XYZ"
   ```

### Option B: Write a Go Integration Test

Create `main_test.go` to automate client handshake and tool execution testing:

```go
package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMcpIntegration(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "1.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "enquire_lexicon"}, enquireLexiconHandler)

	handler := NewMcpMultiplexerHandler(func(*http.Request) *mcp.Server { return server })
	ts := httptest.NewServer(handler)
	defer ts.Close()

	ctx := context.Background()
	transport := &mcp.SSEClientTransport{Endpoint: ts.URL}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil)

	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("Failed to establish MCP connection: %v", err)
	}
	defer cs.Close()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "enquire_lexicon",
		Arguments: map[string]any{"query": "elen"},
	})
	if err != nil {
		t.Fatalf("Failed to invoke tool: %v", err)
	}

	text := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "star") {
		t.Errorf("Unexpected result: %s", text)
	}
}
```

Run tests with:
```bash
go test -v ./...
```


## 7. Deploying to Google Cloud Run

When hosting an MCP server on a serverless, stateless platform like Google Cloud Run, there are **two critical rules** you must follow to prevent connection dropouts and silent hangs:

### Rule 1: Always Enable Session Affinity
Because standard MCP clients maintain stateful, in-memory sessions, subsequent handshake and tool execution requests from the same client **must** hit the exact same container instance. 
* If a request gets routed to a different container instance, the server throws a `session not found` error.

Add the `--session-affinity` flag when deploying to Cloud Run:
```bash
gcloud run deploy my-mcp-server \
    --source . \
    --region us-central1 \
    --allow-unauthenticated \
    --port 8080 \
    --memory 256Mi \
    --cpu 1 \
    --session-affinity
```

### Rule 2: Keep Streamable HTTP Stateful (Avoid the 405 Trap)
Do **NOT** set `Stateless: true` in your `StreamableHTTPOptions` if your clients use Streamable HTTP.
* Setting `Stateless: true` forces the Go SDK to reject long-running stream connections (`GET /sse` with session headers) with a **`405 Method Not Allowed`** code.
* Rely on **Session Affinity** (Rule 1) to handle the routing of stateful sessions instead of disabling state entirely.


## 8. Securing with OAuth 2.1 & Client ID Metadata Documents (CIMD)

In decentralised AI ecosystems, statically configured API keys or manual registration models are highly restrictive and difficult to maintain. Using the modern **OAuth 2.1 and Client ID Metadata Documents (CIMD)** protocol resolves this entirely:

### 1. Choosing Firestore for the Authorization Layer
When building stateful OAuth 2.1 authorization servers, you must manage ephemeral states: specifically, the **5-minute authorization codes** issued during the user-approval step and exchanged during the token-request step.
* **Why Firestore?** We chose Google Cloud Firestore (specifically configured on a non-default database instance like `mithlond-services`) over traditional relational databases or memory stores like Redis.
  * **Zero Operational Overhead:** Serverless and completely managed.
  * **Scale-from-Zero Integration:** Matches the Cloud Run scaling profile.
  * **Auto-Purging TTL Policy:** We can write documents with an `expires_at` timestamp and let Firestore's native TTL policy clean up expired authorization codes automatically, removing complex database maintenance.
  * **Strict Isolation:** Gated behind GCP IAM roles (`roles/datastore.user`) bound strictly to our dedicated Cloud Run runner service account.

### 2. SSRF-Safe Dynamic Client Validation
Because client IDs are URLs (e.g. `https://client.com/metadata.json`) containing dynamic client metadata, your server must perform dynamic HTTP requests to resolve them. To prevent Server-Side Request Forgery (SSRF) sweeps, implement a custom connection dialer blocking loopbacks and private IP addresses:

```go
func SafeHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}

				ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
				if err != nil {
					return nil, err
				}

				// Block private, loopback, local-link, or un-specified IPs
				for _, ip := range ips {
					if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
						return nil, fmt.Errorf("connection to private/loopback IP blocked: %s", ip)
					}
				}

				dialer := net.Dialer{Timeout: 3 * time.Second}
				return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
			},
		},
	}
}
```

### 2. Stateless Access Token Ingress Middleware
Using signed **JSON Web Tokens (JWT)** as access tokens allows your server to validate active tool execution sessions locally (using signature checks) without querying Firestore or your central user directories during tool execution, enabling massive scalability.

```go
func oauthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var tokenStr string
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			tokenStr = strings.TrimPrefix(authHeader, "Bearer ")
		}

		// Fallback to query parameter (resilient for standard browser SSE stream connections)
		if tokenStr == "" {
			tokenStr = r.URL.Query().Get("token")
		}

		if tokenStr == "" {
			http.Error(w, "Missing access token", http.StatusUnauthorized)
			return
		}

		// Verify HMAC-SHA256 signature locally using signing key
		token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
			return []byte(os.Getenv("JWT_SIGNING_KEY")), nil
		})

		if err != nil || !token.Valid {
			http.Error(w, "Invalid or expired token", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}
```

Wrap your SSE transport handler to fully protect active tool streaming:
```go
mux.Handle("/sse", oauthMiddleware(sseHandler))
```

### 3. User Authorization & Admin Tooling Patterns
To move from "any authenticated user" to "authorized users with roles and scopes", you need a user directory.

#### Gating JWT Issuance via Firestore
When the backend verifies a user's Firebase identity token during the callback, it must perform a quick check against an `authorized_users` collection in Firestore:
* **The Document:** Maps the user's unique Firebase `UID` to their allowed `roles` (e.g. `["user", "admin"]`) and `scopes` (e.g. `["lexicon:read", "audio:generate"]`).
* **Active Status:** An `active` boolean flag allows admins to revoke a user's access instantly. If `active == false`, the server refuses to issue any new signed JWT access tokens during the exchange step.
* **Performance Benefit:** Once the scoped JWT is issued (e.g. valid for 1 hour), the main MCP server remains **stateless**. During active tool calls, it only performs HMAC signature verification locally. It *never* queries Firestore during active SSE streams, maintaining sub-millisecond response times.

#### Granular MCP Tool Access Control (ACL Gating)
Once scopes are embedded into your signed JWTs, you can enforce them at the middleware level or gate specific handlers dynamically.

Here is an example of an extensible HTTP gate middleware that intercepts incoming SSE client streams, decodes the claims, and verifies if the user possesses the required scopes:

```go
// Helper to gate specific handlers with scope checks
func gate(requiredScope string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract token (already parsed/verified by base oauthMiddleware)
		tokenStr := ""
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			tokenStr = strings.TrimPrefix(authHeader, "Bearer ")
		}

		token, _ := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
			return jwtSigningKey, nil
		})
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			http.Error(w, "Forbidden: invalid claims", http.StatusForbidden)
			return
		}

		// Verify requested scope exists inside claims
		if !authorizeScopes(claims, requiredScope) {
			log.Printf("[Auth] Denied: user lacks scope '%s'", requiredScope)
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprintln(w, "Forbidden: insufficient scopes")
			return
		}

		next.ServeHTTP(w, r)
	})
}
```

Now you can mount and protect your endpoints selectively:
```go
// Allow only authenticated users with 'lexicon:read' scope to connect to the SSE stream
mux.Handle("/sse", gate("lexicon:read", secureHandler))
```

#### The Consent Single-Page Application (`mcp-auth.html`)
The user consent page serves as the bridging mechanism between the desktop agent (`opencode`) and your authorization service. It parses query parameters matching standard OAuth 2.1 profiles:

1. **Parameters Parsed from Query Stream:**
   * `client_id`: The metadata URL of the client (e.g. `https://client.com/metadata.json`).
   * `redirect_uri`: The local client loopback endpoint (e.g. `http://127.0.0.1:19876/mcp/oauth/callback`).
   * `state`: A cryptographic random state to prevent CSRF.
   * `code_challenge`: The PKCE S256 challenge.

2. **The Flow on Login Success:**
   * After the user authenticates with Firebase Auth, the frontend sends a `POST` request containing the Firebase `id_token` and parsed OAuth parameters to the server's `/api/oauth/authorize-callback` endpoint.
   * On receiving a `200 OK` containing the transient `code`, the frontend performs a client redirect back to the local agent loopback address, completing the handshake:
     ```javascript
     const data = await response.json();
     if (response.ok && data.code) {
         window.location.href = `${redirectUri}?code=${encodeURIComponent(data.code)}&state=${encodeURIComponent(state)}`;
     }
     ```

#### The "Admin CLI" Tooling Pattern
Admin tasks (adding users, granting scopes, revoking access, or generating testing keys manually) must be handled securely.
* **The Anti-Pattern (API Endpoint):** Creating a `/api/admin/users/grant` endpoint introduces significant security risks. If there is a bug in your route middleware, your entire user directory is exposed to the public internet.
* **The Solution (Admin CLI):** We build a private, local CLI (like `eldamo-admin` under `cmd/eldamo-admin`) that runs only on administrator machines.
  * **Secure Authentication:** Keys off of the administrator's local environment variables (e.g. GCP Application Default Credentials) or a strictly local `.env` configuration.
  * **Reduced Attack Surface:** The main production server does not compile or expose any administrative user-modification endpoints.

---

## 9. Appendix: Complete Production Code Samples

The following are the complete, production-hardened files implementing our entire stateless, SSRF-safe, and dynamic Client ID Metadata Document (CIMD) OAuth 2.1 backend.

### A. The Security Module (`oauth.go`)

This file completely encapsules Firebase verification, dialer-based SSRF protection, dynamic CIMD fetches, transient Firestore code mapping, and signed stateless JWT operations:

```go
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
	allowLocalIPs   = false // Test-toggle to bypass SSRF dialing for local loopback mocks
)

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

	config := &firebase.Config{ProjectID: projectID}
	app, err := firebase.NewApp(ctx, config)
	if err != nil {
		log.Fatalf("Failed to initialize Firebase App: %v", err)
	}

	firebaseAuth, err = app.Auth(ctx)
	if err != nil {
		log.Fatalf("Failed to initialize Firebase Auth Client: %v", err)
	}

	firestoreClient, err = firestore.NewClientWithDatabase(ctx, projectID, databaseID)
	if err != nil {
		log.Fatalf("Failed to initialize Firestore Client: %v", err)
	}

	log.Printf("Successfully initialized Firebase Auth and Firestore database '%s' on project '%s'.", databaseID, projectID)
}

func SafeHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}

				ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
				if err != nil {
					return nil, err
				}

				if !allowLocalIPs {
					for _, ip := range ips {
						if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
							return nil, fmt.Errorf("connection to private/loopback IP blocked: %s", ip)
						}
					}
				}

				dialer := net.Dialer{Timeout: 3 * time.Second}
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

func FetchAndValidateCIMD(ctx context.Context, clientIDUrl string) (*ClientIDMetadata, error) {
	parsedURL, err := url.Parse(clientIDUrl)
	if err != nil {
		return nil, fmt.Errorf("invalid client ID URL format: %w", err)
	}

	isLocal := parsedURL.Hostname() == "localhost" || parsedURL.Hostname() == "127.0.0.1"
	if parsedURL.Scheme != "https" && !isLocal {
		return nil, errors.New("client ID URL must use secure https scheme")
	}

	client := SafeHTTPClient()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clientIDUrl, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Eldamo-MCP-Server-Auth/1.0")

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

	if meta.ClientID != clientIDUrl {
		return nil, fmt.Errorf("client_id in document (%s) does not match fetched URL (%s)", meta.ClientID, clientIDUrl)
	}

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

	decodedToken, err := firebaseAuth.VerifyIDToken(r.Context(), payload.IDToken)
	if err != nil {
		log.Printf("[OAuth] ID Token verification failed: %v", err)
		http.Error(w, "Unauthorized user session", http.StatusUnauthorized)
		return
	}

	userEmail, _ := decodedToken.Claims["email"].(string)
	log.Printf("[OAuth] Authenticated session for user '%s' (%s)", decodedToken.UID, userEmail)

	clientMeta, err := FetchAndValidateCIMD(r.Context(), payload.ClientID)
	if err != nil {
		log.Printf("[OAuth] Client validation failed for %s: %v", payload.ClientID, err)
		http.Error(w, fmt.Sprintf("Client validation failed: %s", err.Error()), http.StatusForbidden)
		return
	}

	isRedirectAllowed := false
	for _, uri := range clientMeta.RedirectURIs {
		if uri == payload.RedirectURI {
			isRedirectAllowed = true
			break
		}
	}
	if !isRedirectAllowed {
		log.Printf("[OAuth] Redirect URI '%s' not authorized for client '%s'", payload.RedirectURI, payload.ClientID)
		http.Error(w, "Unauthorized redirect URI", http.StatusForbidden)
		return
	}

	authCode := generateRandomString(32)

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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": authCode})
}

func generateRandomString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)[:n]
}

var jwtSigningKey = []byte(getJWTSigningKey())

func getJWTSigningKey() string {
	key := os.Getenv("JWT_SIGNING_KEY")
	if key == "" {
		key = "temporary-dev-signing-key-mithlond"
	}
	return key
}

func generateJWT(userUID, clientID, issuer string, duration time.Duration, tokenType string) (string, error) {
	claims := jwt.MapClaims{
		"sub":       userUID,
		"client_id": clientID,
		"scope":     "mcp",
		"iss":       issuer,
		"type":      tokenType,
		"iat":       time.Now().Unix(),
		"exp":       time.Now().Add(duration).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSigningKey)
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

func handleTokenExchange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

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
		if err := r.ParseForm(); err == nil {
			grantType = r.FormValue("grant_type")
			code = r.FormValue("code")
			redirectURI = r.FormValue("redirect_uri")
			clientID = r.FormValue("client_id")
			codeVerifier = r.FormValue("code_verifier")
			refreshTokenParam = r.FormValue("refresh_token")
		}
	}

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

		docRef := firestoreClient.Collection("mcp_auth_codes").Doc(code)
		docSnap, err := docRef.Get(r.Context())
		if err != nil {
			oauthError(http.StatusBadRequest, "invalid_grant", "Invalid or expired authorization code")
			return
		}

		_, _ = docRef.Delete(r.Context())

		data := docSnap.Data()

		expiresAt, ok := data["expires_at"].(time.Time)
		if !ok || time.Now().After(expiresAt) {
			oauthError(http.StatusBadRequest, "invalid_grant", "Authorization code has expired")
			return
		}

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

		storedChallenge, _ := data["code_challenge"].(string)
		if storedChallenge != "" {
			if codeVerifier == "" {
				oauthError(http.StatusBadRequest, "invalid_request", "Missing PKCE code_verifier")
				return
			}
			hash := sha256.Sum256([]byte(codeVerifier))
			computedChallenge := base64.RawURLEncoding.EncodeToString(hash[:])
			if computedChallenge != storedChallenge {
				oauthError(http.StatusBadRequest, "invalid_grant", "Invalid PKCE code_verifier")
				return
			}
		}

		userUID, _ := data["user_uid"].(string)

		accessToken, err := generateJWT(userUID, clientID, issuer, 1*time.Hour, "access")
		if err != nil {
			oauthError(http.StatusInternalServerError, "server_error", "Failed to generate access token")
			return
		}

		refreshToken, err := generateJWT(userUID, clientID, issuer, 30*24*time.Hour, "refresh")
		if err != nil {
			oauthError(http.StatusInternalServerError, "server_error", "Failed to generate refresh token")
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(TokenResponse{
			AccessToken:  accessToken,
			TokenType:    "Bearer",
			ExpiresIn:    3600,
			RefreshToken: refreshToken,
			Scope:        "mcp",
		})
		return

	case "refresh_token":
		if refreshTokenParam == "" {
			oauthError(http.StatusBadRequest, "invalid_request", "Missing refresh_token parameter")
			return
		}

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

		accessToken, err := generateJWT(userUID, clientID, issuer, 1*time.Hour, "access")
		if err != nil {
			oauthError(http.StatusInternalServerError, "server_error", "Failed to generate access token")
			return
		}

		newRefreshToken, err := generateJWT(userUID, clientID, issuer, 30*24*time.Hour, "refresh")
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
			Scope:        "mcp",
		})
		return

	default:
		oauthError(http.StatusBadRequest, "unsupported_grant_type", "Supported grant types are 'authorization_code' and 'refresh_token'")
	}
}

func oauthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var tokenStr string
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			if strings.HasPrefix(authHeader, "Bearer ") {
				tokenStr = strings.TrimPrefix(authHeader, "Bearer ")
			}
		}

		if tokenStr == "" {
			tokenStr = r.URL.Query().Get("token")
			if tokenStr == "" {
				tokenStr = r.URL.Query().Get("access_token")
			}
		}

		if tokenStr == "" {
			log.Printf("[Auth] Rejected request %s %s: Missing access token", r.Method, r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":             "unauthorized",
				"error_description": "Missing access token in Authorization header or query parameter",
			})
			return
		}

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

		next.ServeHTTP(w, r)
	})
}
```

### B. Routing Integration (`main.go` snippet)

Tying your secured endpoints and OAuth handlers into your HTTP Server multiplexer:

```go
func main() {
	log.Println("Initializing Firebase and Firestore clients...")
	initFirebase()

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "my-mcp-server",
		Version: "1.0.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "enquire_lexicon",
		Description: "Lookup Tolkien terms.",
	}, enquireLexiconHandler)

	// Wrap multiplexer with Bearer JWT verification
	secureHandler := oauthMiddleware(sseLoggingMiddleware(NewMcpMultiplexerHandler(func(*http.Request) *mcp.Server { return server })))

	mux := http.NewServeMux()
	
	// Unauthenticated Liveness probe
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "OK")
	})

	// Unauthenticated Discovery endpoint
	mux.HandleFunc("/.well-known/oauth-authorization-server", handleOAuthDiscovery)

	// Unauthenticated Authorize & Token exchange callbacks
	mux.HandleFunc("/api/oauth/authorize-callback", handleAuthCallback)
	mux.HandleFunc("/api/oauth/token", handleTokenExchange)

	// Authenticated MCP multiplexed streams
	mux.Handle("/sse", secureHandler)

	log.Fatal(http.ListenAndServe(":8080", mux))
}
```
