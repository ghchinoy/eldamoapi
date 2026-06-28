package main

import (
	"context"
	"iter"
	"log"
	"net/http"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// a2aBasePath is the HTTP path where the A2A JSON-RPC transport is mounted.
const a2aBasePath = "/a2a"

// echoAgentExecutor is a minimal AgentExecutor used for the Phase 1 wiring spike.
// It echoes the incoming message text back to the caller. Real deterministic
// skills (name generation, lexicon lookups) will replace/extend this later.
type echoAgentExecutor struct{}

var _ a2asrv.AgentExecutor = (*echoAgentExecutor)(nil)

func (*echoAgentExecutor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		var b strings.Builder
		if execCtx != nil && execCtx.Message != nil {
			for _, p := range execCtx.Message.Parts {
				if t := p.Text(); t != "" {
					b.WriteString(t)
				}
			}
		}
		text := strings.TrimSpace(b.String())
		if text == "" {
			text = "(empty message)"
		}
		log.Printf("[A2A] echo executor received: %q", text)
		reply := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("Echo from Eldamo: "+text))
		yield(reply, nil)
	}
}

func (*echoAgentExecutor) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {}
}

// buildAgentCard constructs the public AgentCard for the given absolute base URL
// (e.g. "https://host"). The JSON-RPC interface URL is derived from the base URL.
func buildAgentCard(baseURL string) *a2a.AgentCard {
	return &a2a.AgentCard{
		Name:        "Eldamo Elvish Agent",
		Description: "Agentic access to Paul Strack's Eldamo Tolkien-language lexicon: search, derivations, and (forthcoming) name-generation and translation skills.",
		Version:     "0.1.0",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(baseURL+a2aBasePath, a2a.TransportProtocolJSONRPC),
		},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Capabilities:       a2a.AgentCapabilities{Streaming: true},
		Skills: []a2a.AgentSkill{
			{
				ID:          "echo",
				Name:        "Echo",
				Description: "Phase 1 wiring spike: echoes the supplied text back.",
				Tags:        []string{"diagnostic"},
				Examples:    []string{"hello", "elen sila"},
			},
		},
	}
}

// requestBaseURL derives the public scheme://host for the current request,
// mirroring the logic in handleOAuthDiscovery so AgentCard URLs are correct
// both locally and behind the Cloud Run / GFE proxy.
func requestBaseURL(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil && (strings.HasPrefix(r.Host, "localhost:") || strings.HasPrefix(r.Host, "127.0.0.1:")) {
		scheme = "http"
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return scheme + "://" + host
}

// handleAgentCard serves the public, unauthenticated A2A AgentCard at the
// well-known path. The card itself is public discovery metadata; the protocol
// endpoint it points to is protected by oauthMiddleware.
func handleAgentCard(w http.ResponseWriter, r *http.Request) {
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
	card := buildAgentCard(requestBaseURL(r))
	a2asrv.NewStaticAgentCardHandler(card).ServeHTTP(w, r)
}

// newA2AHandler builds the transport-agnostic A2A request handler and wraps it
// in the JSON-RPC HTTP transport binding. Returned as a plain http.Handler so it
// can be mounted on the shared mux behind the existing oauthMiddleware.
func newA2AHandler() http.Handler {
	requestHandler := a2asrv.NewHandler(&echoAgentExecutor{})
	return a2asrv.NewJSONRPCHandler(requestHandler)
}
