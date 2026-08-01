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
	"time"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
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

type GetRootAnchorsArgs struct {
	ID string `json:"id" jsonschema:"The unique page-id of the root or base word (e.g., '2071154627' for root LIK)"`
}

type RenderElvishAudioArgs struct {
	Text  string  `json:"text" jsonschema:"The Elvish word or phrase to pronounce."`
	Voice string  `json:"voice,omitempty" jsonschema:"Optional voice identifier (e.g., 'sarah', 'bella', 'adam')."`
	Speed float64 `json:"speed,omitempty" jsonschema:"Optional speed, default 0.8."`
}

type EnquireLexiconResult struct {
	Total   int               `json:"total"`
	Count   int               `json:"count"`
	Matches []*index.FlatWord `json:"matches"`
}

type GetWordDetailsResult struct {
	Word *index.FlatWord `json:"word"`
}

type GetDerivationsResult struct {
	ID        string            `json:"id"`
	Direction string            `json:"direction"`
	Count     int               `json:"count"`
	Results   []*index.FlatWord `json:"results"`
}

type GetRootAnchorsResult struct {
	ID      string            `json:"id"`
	Count   int               `json:"count"`
	Anchors []*index.FlatWord `json:"anchors"`
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

func enquireLexiconHandler(ctx context.Context, req *mcp.CallToolRequest, args EnquireLexiconArgs) (*mcp.CallToolResult, *EnquireLexiconResult, error) {
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
	out := &EnquireLexiconResult{
		Total:   len(combined),
		Count:   limit,
		Matches: results,
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: msg},
		},
	}, out, nil
}

func getWordDetailsHandler(ctx context.Context, req *mcp.CallToolRequest, args GetWordDetailsArgs) (*mcp.CallToolResult, *GetWordDetailsResult, error) {
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
	out := &GetWordDetailsResult{Word: word}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: msg},
		},
	}, out, nil
}

func getDerivationsHandler(ctx context.Context, req *mcp.CallToolRequest, args GetDerivationsArgs) (*mcp.CallToolResult, *GetDerivationsResult, error) {
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
	out := &GetDerivationsResult{
		ID:        id,
		Direction: dir,
		Count:     len(derivs),
		Results:   derivs,
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: msg},
		},
	}, out, nil
}

func getRootAnchorsHandler(ctx context.Context, req *mcp.CallToolRequest, args GetRootAnchorsArgs) (*mcp.CallToolResult, *GetRootAnchorsResult, error) {
	id := strings.TrimSpace(args.ID)
	log.Printf("[Tool Call] get_root_anchors: id='%s'", id)
	if id == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: id cannot be empty"},
			},
			IsError: true,
		}, nil, nil
	}

	anchors := lexiconIndex.GetRootAnchors(id)
	if len(anchors) == 0 {
		log.Printf("[Tool Result] get_root_anchors: found 0 anchors for word ID '%s'", id)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("No proper-noun or place name anchors found derived from ID '%s'.", id)},
			},
		}, nil, nil
	}

	resBytes, err := json.MarshalIndent(anchors, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal root anchors: %w", err)
	}

	log.Printf("[Tool Result] get_root_anchors: found %d anchors for word ID '%s'", len(anchors), id)
	msg := fmt.Sprintf("Found %d character/place name anchors derived from ID '%s':\n\n```json\n%s\n```", len(anchors), id, string(resBytes))
	out := &GetRootAnchorsResult{
		ID:      id,
		Count:   len(anchors),
		Anchors: anchors,
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: msg},
		},
	}, out, nil
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
	// Support selective SSE transport override (e.g. for Antigravity Desktop on Cloud
	// Run / GFE buffering). Restricted to GET: the override exists solely to force the
	// long-lived streaming connection onto the legacy SSE handler, which is what
	// actually suffers from GFE response buffering. POST/DELETE must always fall
	// through to the session-based auto-detection below, regardless of this header.
	//
	// Some Streamable-HTTP-only clients (e.g. opencode, Antigravity CLI/agy) send this
	// header on every request — including their very first "initialize" call, a bare
	// POST with no prior GET and no session established. The legacy SSE handler
	// requires a session created by a prior GET, so blindly honoring this header for
	// POST incorrectly 400s that handshake ("sending initialize: Bad Request") even
	// though the client never intended to speak the legacy SSE transport at all.
	if r.Method == http.MethodGet && strings.ToLower(r.Header.Get("X-Mcp-Force-Sse")) == "true" {
		log.Printf("[Multiplexer] SSE force-override header detected. Routing strictly to SSEHandler: %s %s", r.Method, r.URL.RequestURI())
		h.sseHandler.ServeHTTP(w, r)
		return
	}

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
		"authorization_endpoint":                "https://www.mithlond.com/mcp-auth",
		"token_endpoint":                        fmt.Sprintf("%s/api/oauth/token", baseURL),
		"registration_endpoint":                 fmt.Sprintf("%s/api/oauth/register", baseURL),
		"service_documentation":                 "https://github.com/ghchinoy/eldamoapi",
		"client_id_metadata_document_supported": true,
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

// protectedResourceMetadataPrefix is the RFC 9728 well-known base path. Clients
// probe this path directly, and also with the resource path inserted after it
// (e.g. .../oauth-protected-resource/sse), so we serve both the exact path and
// the subtree from handleProtectedResourceMetadata.
const protectedResourceMetadataPrefix = "/.well-known/oauth-protected-resource"

// handleProtectedResourceMetadata serves RFC 9728 (OAuth 2.0 Protected Resource
// Metadata). Modern MCP clients (Gemini Spark, opencode) fetch this document to
// discover which authorization server protects the resource BEFORE they reach
// the RFC 8414 authorization-server metadata. It is public/unauthenticated per
// the "public discovery, protected protocol" principle; the /sse and /a2a
// endpoints it advertises remain JWT-gated by oauthMiddleware.
//
// Clients insert the resource path into the well-known path per RFC 9728
// (e.g. GET /.well-known/oauth-protected-resource/sse), so we derive the
// specific resource identifier from the path suffix and also answer the bare
// /.well-known/oauth-protected-resource path.
func handleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	baseURL := requestBaseURL(r)

	// Derive the specific resource identifier from any path suffix the client
	// appended (e.g. "/sse" or "/a2a"). A bare or trailing-slash path maps to
	// the base URL as the resource identifier.
	resource := baseURL
	if suffix := strings.TrimPrefix(r.URL.Path, protectedResourceMetadataPrefix); suffix != "" && suffix != "/" {
		resource = baseURL + suffix
	}

	// Both /sse (MCP) and /a2a (A2A) sit behind the same oauthMiddleware and are
	// protected by the same authorization server (this host's RFC 8414 document),
	// so a single PRM shape describes every resource — "one token, both protocols".
	metadata := map[string]any{
		"resource":                 resource,
		"authorization_servers":    []string{baseURL},
		"scopes_supported":         []string{"lexicon:read"},
		"bearer_methods_supported": []string{"header"},
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(metadata); err != nil {
		log.Printf("Error encoding protected resource metadata: %v", err)
	}
}

// createMCPServer instantiates and configures the MCP server with all tools,
// prompts, and resources.
func createMCPServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "eldamo-mcp-server",
		Version: "0.2.0",
	}, &mcp.ServerOptions{
		KeepAlive: 30 * time.Second,
	})

	// Register tools using type-safe AddTool helper
	mcp.AddTool(server, &mcp.Tool{
		Name:        "enquire_lexicon",
		Title:       "Search Lexicon",
		Description: "Search the Eldamo Tolkien lexicon. Combines prefix spelling search and full-text keyword search across words, glosses, and notes. Results are capped at 50.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, enquireLexiconHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_word_details",
		Title:       "Word Details",
		Description: "Fetch complete details for a specific Eldamo entry by its unique page ID.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, getWordDetailsHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_derivations",
		Title:       "Derivation Tree",
		Description: "Retrieve derivation history (ancestors or descendants) of a word by its unique page ID.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, getDerivationsHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_root_anchors",
		Title:       "Root Anchors",
		Description: "Retrieve proper names (characters, places, etc.) recursively derived from a specific root or base word ID.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, getRootAnchorsHandler)

	if os.Getenv("ELVISH_TTS_URL") != "" {
		mcp.AddTool(server, &mcp.Tool{
			Name:        "render_elvish_audio",
			Title:       "Pronounce Elvish",
			Description: "Synthesizes pronunciation for Elvish words or phrases using Kokoro-based TTS.",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		}, renderElvishAudioHandler)
	}

	// --- MCP Prompts ---
	// Register the 3 linguistic workflow prompts so any MCP client can invoke them.
	server.AddPrompt(&mcp.Prompt{
		Name:        "tolkien-translation",
		Title:       "Tolkien Elvish Translation",
		Description: "Translates English text into Quenya or Sindarin, applying morphology, case endings, and consonant mutations.",
		Arguments: []*mcp.PromptArgument{
			{Name: "text", Title: "Text to Translate", Description: "English text or phrase to translate", Required: true},
			{Name: "language", Title: "Target Language", Description: "Optional language filter ('q' for Quenya, 's' for Sindarin)"},
		},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		text := req.Params.Arguments["text"]
		lang := req.Params.Arguments["language"]
		promptText := translateSkillMD
		if lang != "" {
			promptText += fmt.Sprintf("\n\nTarget Language: %s", lang)
		}
		if text != "" {
			promptText += fmt.Sprintf("\n\nText to translate: %s", text)
		}
		return &mcp.GetPromptResult{
			Description: "Tolkien Elvish Translation Workflow Prompt",
			Messages: []*mcp.PromptMessage{
				{
					Role:    "user",
					Content: &mcp.TextContent{Text: promptText},
				},
			},
		}, nil
	})

	server.AddPrompt(&mcp.Prompt{
		Name:        "tolkien-name-generator",
		Title:       "Tolkien Elvish Name Generator",
		Description: "Generates grammatically correct, historically authentic Tolkien Elvish names for characters, places, stars, or weapons.",
		Arguments: []*mcp.PromptArgument{
			{Name: "concepts", Title: "Name Concepts", Description: "Keywords or concepts for the name (e.g., 'star silver')", Required: true},
			{Name: "language", Title: "Language", Description: "Optional language ('quenya' or 'sindarin')"},
			{Name: "gender", Title: "Gender/Type", Description: "Optional gender or category (masculine, feminine, place, celestial)"},
		},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		concepts := req.Params.Arguments["concepts"]
		lang := req.Params.Arguments["language"]
		gender := req.Params.Arguments["gender"]
		promptText := nameGenSkillMD
		if concepts != "" {
			promptText += fmt.Sprintf("\n\nName concept / request: %s", concepts)
		}
		if lang != "" {
			promptText += fmt.Sprintf(" (Language: %s)", lang)
		}
		if gender != "" {
			promptText += fmt.Sprintf(" (Gender/Type: %s)", gender)
		}
		return &mcp.GetPromptResult{
			Description: "Tolkien Elvish Name Generator Workflow Prompt",
			Messages: []*mcp.PromptMessage{
				{
					Role:    "user",
					Content: &mcp.TextContent{Text: promptText},
				},
			},
		}, nil
	})

	server.AddPrompt(&mcp.Prompt{
		Name:        "neologism-builder",
		Title:       "Elvish Neologism Builder",
		Description: "Guides the creation of Neo-Elvish vocabulary using two stylistic paths, the Anchorage Protocol, and a 100-point scoring matrix.",
		Arguments: []*mcp.PromptArgument{
			{Name: "concept", Title: "Concept to Coin", Description: "The modern concept or term to construct an Elvish word for (e.g., 'hover-board')", Required: true},
			{Name: "language", Title: "Language", Description: "Optional target language ('quenya' or 'sindarin')"},
		},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		concept := req.Params.Arguments["concept"]
		lang := req.Params.Arguments["language"]
		promptText := neologismSkillMD
		if concept != "" {
			promptText += fmt.Sprintf("\n\nConcept to coin: %s", concept)
		}
		if lang != "" {
			promptText += fmt.Sprintf(" (Language: %s)", lang)
		}
		return &mcp.GetPromptResult{
			Description: "Elvish Neologism Builder Workflow Prompt",
			Messages: []*mcp.PromptMessage{
				{
					Role:    "user",
					Content: &mcp.TextContent{Text: promptText},
				},
			},
		}, nil
	})

	// --- MCP Resources ---
	// Expose AgentCard and lexicon statistics as read-only MCP resources.
	server.AddResource(&mcp.Resource{
		URI:         "eldamo://agent-card",
		Name:        "AgentCard",
		Title:       "Eldamo AgentCard",
		Description: "Public AgentCard JSON describing agent capabilities and skills.",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		card := buildExtendedAgentCard("https://candir.mithlond.com")
		data, err := json.MarshalIndent(card, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("failed to marshal AgentCard resource: %w", err)
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{
				{
					URI:      "eldamo://agent-card",
					MIMEType: "application/json",
					Text:     string(data),
				},
			},
		}, nil
	})

	server.AddResource(&mcp.Resource{
		URI:         "eldamo://lexicon/stats",
		Name:        "LexiconStats",
		Title:       "Lexicon Statistics",
		Description: "Summary statistics for the loaded Eldamo in-memory lexicon database.",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		stats := map[string]any{
			"total_words": len(lexiconIndex.Words),
			"keywords":    len(lexiconIndex.InvertedMap),
		}
		data, err := json.MarshalIndent(stats, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("failed to marshal lexicon stats resource: %w", err)
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{
				{
					URI:      "eldamo://lexicon/stats",
					MIMEType: "application/json",
					Text:     string(data),
				},
			},
		}, nil
	})

	return server
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

	server := createMCPServer()

	// Create multiplexed handler to support both SSE and Streamable HTTP transports
	handler := NewMcpMultiplexerHandler(func(*http.Request) *mcp.Server { return server })

	// gate wraps a handler with a scope check. Claims are already verified and
	// stashed in the context by oauthMiddleware — no second JWT parse needed.
	gate := func(requiredScope string, next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := ClaimsFromContext(r.Context())
			if !ok || !authorizeScopes(claims, requiredScope) {
				log.Printf("[Auth] Denied scope '%s' for %s %s", requiredScope, r.Method, r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error":             "insufficient_scope",
					"error_description": "Token does not have required scope: " + requiredScope,
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	// oauthMiddleware must run first to stash claims, then gate reads them.
	// Order: oauthMiddleware → sseLoggingMiddleware → gate(scope) → mcpHandler
	secureHandler := oauthMiddleware(sseLoggingMiddleware(gate("lexicon:read", handler)))

	// Support simple Liveness probe
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(w, "OK")
	})

	// Mount the well-known OAuth 2.1 discovery endpoint (RFC 8414)
	mux.HandleFunc("/.well-known/oauth-authorization-server", handleOAuthDiscovery)

	// Mount the RFC 9728 Protected Resource Metadata endpoint. The exact path
	// serves the base resource; the trailing-slash subtree catches the
	// resource-path-suffixed variants clients probe (e.g. .../oauth-protected-resource/sse).
	mux.HandleFunc(protectedResourceMetadataPrefix, handleProtectedResourceMetadata)
	mux.HandleFunc(protectedResourceMetadataPrefix+"/", handleProtectedResourceMetadata)

	// Mount the OAuth 2.1 authorize-callback endpoint
	mux.HandleFunc("/api/oauth/authorize-callback", handleAuthCallback)

	// Mount the OAuth 2.1 token endpoint
	mux.HandleFunc("/api/oauth/token", handleTokenExchange)

	// Mount the RFC 7591 Dynamic Client Registration endpoint. Coexists with
	// CIMD; issues public (PKCE) client_ids so DCR-only clients (e.g. Spark)
	// can self-register. Proxied by mithlond-web's /api/oauth/** Hosting rewrite.
	mux.HandleFunc("/api/oauth/register", handleClientRegistration)

	// Mount the MCP handler — the scope gate is baked into secureHandler already.
	mux.Handle("/sse", secureHandler)

	// Also expose the MCP transport at the exact base path. Some clients treat
	// the server URL as a single Streamable HTTP endpoint and probe the root
	// directly (Gemini Spark issues POST / and HEAD /), which otherwise 404s.
	// The "/{$}" pattern matches ONLY "/" (Go 1.22+), so unknown paths still
	// 404 as before. Same secureHandler chain (oauthMiddleware → sseLogging →
	// gate) — base-URL probes now get a proper 401 challenge with the RFC 9728
	// resource_metadata pointer instead of a bare 404, and authenticated clients
	// can drive the full transport from the root.
	mux.Handle("/{$}", secureHandler)

	// --- A2A (Agent2Agent) exposure ---
	// Public, unauthenticated AgentCard discovery document.
	mux.HandleFunc(a2asrv.WellKnownAgentCardPath, handleAgentCard)
	// Protected A2A JSON-RPC endpoint, behind the same OAuth verifier and SSE
	// header/logging middleware as the MCP transport.
	a2aHandler := oauthMiddleware(sseLoggingMiddleware(newA2AHandler()))
	mux.Handle(a2aBasePath, a2aHandler)
	mux.Handle(a2aBasePath+"/", a2aHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Eldamo MCP Server listening on port %s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("Server shutdown failed: %v", err)
	}
}
