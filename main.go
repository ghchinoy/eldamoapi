package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/ghchinoy/eldamoapi/data"
	"github.com/ghchinoy/eldamoapi/index"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var lexiconIndex *index.Index

type EnquireLexiconArgs struct {
	Query    string `json:"query" jsonschema:"The keyword or prefix to search for (e.g., 'star', 'flower', 'elendil', 'elen')"`
	Language string `json:"language,omitempty" jsonschema:"Optional language code (e.g., 'q' for Quenya, 's' for Sindarin, 'pc' for Primitive Elvish, 'on' for Old Noldorin)"`
	Speech   string `json:"speech,omitempty" jsonschema:"Optional part of speech filter (e.g., 'noun', 'verb', 'adjective', 'proper-name')"`
	Category string `json:"category,omitempty" jsonschema:"Optional category/era filter (e.g., 'neo', 'primary', 'root')"`
}

type GetWordDetailsArgs struct {
	ID string `json:"id" jsonschema:"The unique page-id of the word (e.g., '218765')"`
}

type GetDerivationsArgs struct {
	ID        string `json:"id" jsonschema:"The unique page-id of the word (e.g., '218765')"`
	Direction string `json:"direction,omitempty" jsonschema:"The direction of derivation: 'ancestors' (what this word was derived from) or 'descendants' (what words were derived from this word). Defaults to 'descendants'"`
}

type RenderElvishAudioArgs struct {
	Text  string  `json:"text" jsonschema:"The Elvish word or phrase to pronounce."`
	Voice string  `json:"voice,omitempty" jsonschema:"Optional voice identifier (e.g., 'sarah', 'bella', 'adam')."`
	Speed float64 `json:"speed,omitempty" jsonschema:"Optional speed, default 0.8."`
}

func renderElvishAudioHandler(ctx context.Context, req *mcp.CallToolRequest, args RenderElvishAudioArgs) (*mcp.CallToolResult, any, error) {
	ttsURL := os.Getenv("ELVISH_TTS_URL")
	if ttsURL == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: TTS_SERVICE_URL not configured."},
			},
			IsError: true,
		}, nil, nil
	}

	speed := args.Speed
	if speed == 0 {
		speed = 0.8 // Default to slightly slower
	}

	payload := map[string]interface{}{
		"text":  args.Text,
		"voice": args.Voice,
		"speed": speed,
	}
	if payload["voice"] == "" {
		payload["voice"] = "sarah"
	}

	body, _ := json.Marshal(payload)
	resp, err := http.Post(ttsURL+"/api/g2p", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to call TTS service: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("TTS service returned status: %d", resp.StatusCode)
	}

	var ttsResp struct {
		AudioURL string `json:"audio_url"`
		Phonemes string `json:"phonemes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ttsResp); err != nil {
		return nil, nil, fmt.Errorf("failed to decode TTS response: %w", err)
	}

	fullAudioURL := ttsURL + ttsResp.AudioURL
	msg := fmt.Sprintf("Synthesized pronunciation for '%s' (speed: %.1f).\n\nPhonemes: %s\nAudio URL: %s", args.Text, speed, ttsResp.Phonemes, fullAudioURL)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: msg},
		},
	}, nil, nil
}

func enquireLexiconHandler(ctx context.Context, req *mcp.CallToolRequest, args EnquireLexiconArgs) (*mcp.CallToolResult, any, error) {
	query := strings.TrimSpace(args.Query)
	
	var filters []string
	if args.Language != "" {
		filters = append(filters, fmt.Sprintf("language='%s'", args.Language))
	}
	if args.Speech != "" {
		filters = append(filters, fmt.Sprintf("speech='%s'", args.Speech))
	}
	if args.Category != "" {
		filters = append(filters, fmt.Sprintf("category='%s'", args.Category))
	}
	filterStr := "none"
	if len(filters) > 0 {
		filterStr = strings.Join(filters, ", ")
	}
	
	log.Printf("[Tool Call] enquire_lexicon: query='%s' (filters: %s)", query, filterStr)
	if query == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: query cannot be empty"},
			},
			IsError: true,
		}, nil, nil
	}

	var combined []*index.FlatWord
	seen := make(map[string]bool)

	// 1. Inverted index keyword search
	kResults := lexiconIndex.SearchKeyword(query, args.Language, args.Speech, args.Category)
	for _, w := range kResults {
		if !seen[w.ID] {
			seen[w.ID] = true
			combined = append(combined, w)
		}
	}

	// 2. Prefix spelling search
	pResults := lexiconIndex.SearchPrefix(query, args.Language, args.Speech, args.Category)
	for _, w := range pResults {
		if !seen[w.ID] {
			seen[w.ID] = true
			combined = append(combined, w)
		}
	}

	if len(combined) == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("No matches found for query '%s' with the specified filters.", query)},
			},
		}, nil, nil
	}

	// Cap total results at 50
	limit := 50
	if len(combined) < limit {
		limit = len(combined)
	}
	results := combined[:limit]

	resBytes, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal search results: %w", err)
	}

	log.Printf("[Tool Result] enquire_lexicon: found %d matches (returned top %d) for query='%s'", len(combined), limit, query)
	msg := fmt.Sprintf("Found %d matches (showing top %d):\n\n```json\n%s\n```", len(combined), limit, string(resBytes))
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: msg},
		},
	}, nil, nil
}

func getWordDetailsHandler(ctx context.Context, req *mcp.CallToolRequest, args GetWordDetailsArgs) (*mcp.CallToolResult, any, error) {
	id := strings.TrimSpace(args.ID)
	log.Printf("[Tool Call] get_word_details: id='%s'", id)
	if id == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: id cannot be empty"},
			},
			IsError: true,
		}, nil, nil
	}

	word, found := lexiconIndex.GetWord(id)
	if !found {
		log.Printf("[Tool Result] get_word_details: word ID '%s' not found", id)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Word with ID '%s' not found.", id)},
			},
		}, nil, nil
	}

	resBytes, err := json.MarshalIndent(word, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal word details: %w", err)
	}

	log.Printf("[Tool Result] get_word_details: found details for word ID '%s'", id)
	msg := fmt.Sprintf("Details for word ID '%s':\n\n```json\n%s\n```", id, string(resBytes))
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: msg},
		},
	}, nil, nil
}

func getDerivationsHandler(ctx context.Context, req *mcp.CallToolRequest, args GetDerivationsArgs) (*mcp.CallToolResult, any, error) {
	id := strings.TrimSpace(args.ID)
	log.Printf("[Tool Call] get_derivations: id='%s', direction='%s'", id, args.Direction)
	if id == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: id cannot be empty"},
			},
			IsError: true,
		}, nil, nil
	}

	dir := strings.ToLower(strings.TrimSpace(args.Direction))
	if dir != "ancestors" {
		dir = "descendants"
	}

	derivs := lexiconIndex.GetDerivations(id, dir)
	if len(derivs) == 0 {
		log.Printf("[Tool Result] get_derivations: found 0 %s for word ID '%s'", dir, id)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("No %s found for word ID '%s'.", dir, id)},
			},
		}, nil, nil
	}

	resBytes, err := json.MarshalIndent(derivs, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal derivations: %w", err)
	}

	log.Printf("[Tool Result] get_derivations: found %d %s for word ID '%s'", len(derivs), dir, id)
	msg := fmt.Sprintf("Found %d %s for word ID '%s':\n\n```json\n%s\n```", len(derivs), dir, id, string(resBytes))
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: msg},
		},
	}, nil, nil
}

func sseLoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[HTTP Request] %s %s from %s (User-Agent: %s)", r.Method, r.URL.Path, r.RemoteAddr, r.UserAgent())
		
		// Ensure Cloud Run/GFE does not buffer Server-Sent Events (SSE)
		w.Header().Set("X-Accel-Buffering", "no")

		next.ServeHTTP(w, r)
	})
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
	hasSessionID := false
	for k := range r.URL.Query() {
		if strings.ToLower(k) == "sessionid" {
			hasSessionID = true
			break
		}
	}

	// Route based on Streamable HTTP vs SSE traits
	if r.Header.Get("Mcp-Session-Id") != "" ||
		r.Method == "DELETE" ||
		(r.Method == "POST" && !hasSessionID) {
		log.Printf("[Multiplexer] Routing to StreamableHTTPHandler: %s %s", r.Method, r.URL.RequestURI())
		h.streamableHandler.ServeHTTP(w, r)
		return
	}

	log.Printf("[Multiplexer] Routing to SSEHandler: %s %s", r.Method, r.URL.RequestURI())
	h.sseHandler.ServeHTTP(w, r)
}

func handleOAuthDiscovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Dynamically determine scheme (HTTPS unless local development)
	scheme := "https"
	if r.TLS == nil && (strings.HasPrefix(r.Host, "localhost:") || strings.HasPrefix(r.Host, "127.0.0.1:")) {
		scheme = "http"
	}

	// Use standard X-Forwarded-Host or Host
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}

	baseURL := fmt.Sprintf("%s://%s", scheme, host)

	// Build OAuth 2.1 Server Metadata
	metadata := map[string]any{
		"issuer":                                baseURL,
		"authorization_endpoint":                fmt.Sprintf("%s/mcp-auth", baseURL),
		"token_endpoint":                        fmt.Sprintf("%s/api/oauth/token", baseURL),
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(metadata); err != nil {
		log.Printf("Error encoding discovery metadata: %v", err)
	}
}

func main() {
	log.Println("Initializing Firebase and Firestore clients...")
	initFirebase()

	log.Println("Initializing Eldamo in-memory index...")
	rawBytes, err := data.GetJSONL()
	if err != nil {
		log.Fatalf("Failed to decompress embedded dataset: %v", err)
	}
	lexiconIndex, err = index.NewIndex(rawBytes)
	if err != nil {
		log.Fatalf("Failed to initialize search index: %v", err)
	}
	log.Printf("Successfully loaded search index with %d words.", len(lexiconIndex.Words))

	// Instantiate the MCP server
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "eldamo-mcp-server",
		Version: "1.0.0",
	}, nil)

	// Register tools using type-safe AddTool helper
	mcp.AddTool(server, &mcp.Tool{
		Name:        "enquire_lexicon",
		Description: "Search the Eldamo Tolkien lexicon. Combines prefix spelling search and full-text keyword search across words, glosses, and notes. Results are capped at 50.",
	}, enquireLexiconHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_word_details",
		Description: "Fetch complete details for a specific Eldamo entry by its unique page ID.",
	}, getWordDetailsHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_derivations",
		Description: "Retrieve derivation history (ancestors or descendants) of a word by its unique page ID.",
	}, getDerivationsHandler)

	if os.Getenv("ELVISH_TTS_URL") != "" {
		mcp.AddTool(server, &mcp.Tool{
			Name:        "render_elvish_audio",
			Description: "Synthesizes pronunciation for Elvish words or phrases using Kokoro-based TTS.",
		}, renderElvishAudioHandler)
	}

	// Create multiplexed handler to support both SSE and Streamable HTTP transports
	handler := NewMcpMultiplexerHandler(func(*http.Request) *mcp.Server { return server })

	// Wrap handler with logging/SSE headers middleware and OAuth Bearer token verification
	secureHandler := oauthMiddleware(sseLoggingMiddleware(handler))

	// Support simple Liveness probe
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(w, "OK")
	})

	// Mount the well-known OAuth 2.1 discovery endpoint
	mux.HandleFunc("/.well-known/oauth-authorization-server", handleOAuthDiscovery)

	// Mount the OAuth 2.1 authorize-callback endpoint
	mux.HandleFunc("/api/oauth/authorize-callback", handleAuthCallback)

	// Mount the OAuth 2.1 token endpoint
	mux.HandleFunc("/api/oauth/token", handleTokenExchange)

	// Mount the SSE handler to /sse
	mux.Handle("/sse", secureHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Eldamo MCP Server listening on port %s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("Server shutdown failed: %v", err)
	}
}
