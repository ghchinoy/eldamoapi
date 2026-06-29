package main

import (
	"context"
	"fmt"
	"iter"
	"log"
	"net/http"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// a2aBasePath is the HTTP path where the A2A JSON-RPC transport is mounted.
const a2aBasePath = "/a2a"

// hasScope reports whether user holds the named scope.
// Scopes are stored in user.Attributes["scopes"] as []string by claimsInterceptor.
func hasScope(user *a2asrv.User, scope string) bool {
	if user == nil || !user.Authenticated {
		return false
	}
	scopes, _ := user.Attributes["scopes"].([]string)
	for _, s := range scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// claimsInterceptor is an a2asrv.CallInterceptor that:
//  1. Reads the verified JWT claims stashed by oauthMiddleware from the context.
//  2. Populates CallContext.User so the AgentExecutor knows who is calling.
//  3. Enforces the coarse "agent:invoke" scope gate before the executor runs.
type claimsInterceptor struct {
	a2asrv.PassthroughCallInterceptor
}

func (ci *claimsInterceptor) Before(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) (context.Context, any, error) {
	claims, ok := ClaimsFromContext(ctx)
	if !ok {
		// oauthMiddleware always stashes claims (including the AUTH_BYPASS path).
		// If they're missing, something is wired incorrectly — fail closed.
		return ctx, nil, fmt.Errorf("no auth claims in context; check middleware chain")
	}

	sub, _ := claims["sub"].(string)
	scopes := scopesSlice(claims)
	callCtx.User = a2asrv.NewAuthenticatedUser(sub, map[string]any{"scopes": scopes})

	if !hasScope(callCtx.User, "agent:invoke") {
		log.Printf("[A2A] Denied agent:invoke for user %q (scopes: %v)", sub, scopes)
		return ctx, nil, fmt.Errorf("insufficient scope: agent:invoke required")
	}

	log.Printf("[A2A] Authorized user %q scopes=%v", sub, scopes)
	return ctx, nil, nil
}

// eldamoAgentExecutor dispatches incoming messages to the appropriate skill
// executor based on intent detected in the message text. Scope checks for
// per-skill gates happen here (coarse agent:invoke is already handled by
// claimsInterceptor before Execute is called).
//
// Routing rules:
//
//	message starts with "name " OR contains a language keyword → name-generate
//	everything else                                            → echo
type eldamoAgentExecutor struct{}

var _ a2asrv.AgentExecutor = (*eldamoAgentExecutor)(nil)

func (e *eldamoAgentExecutor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	// Order matters: translate and neologism have specific trigger prefixes;
	// check them before name-generate which fires on any language keyword.
	if isNeologismRequest(execCtx.Message) {
		if !TranslateEnabled() {
			return scopeRejection("")
		}
		if !hasScope(execCtx.User, "skill:neologism") {
			return scopeRejection("skill:neologism")
		}
		return runNeologism(ctx, execCtx)
	}
	if isTranslateRequest(execCtx.Message) {
		if !TranslateEnabled() {
			return scopeRejection("")
		}
		if !hasScope(execCtx.User, "skill:translate") {
			return scopeRejection("skill:translate")
		}
		return runTranslate(ctx, execCtx)
	}
	if isNameRequest(execCtx.Message) {
		if !hasScope(execCtx.User, "skill:name-generate") {
			return scopeRejection("skill:name-generate")
		}
		return runNameGenerate(ctx, execCtx)
	}
	return runEcho(ctx, execCtx)
}

// scopeRejection returns an executor func that yields a plain rejection Message.
// Using a bare *Message (no task registration) keeps the response simple and
// avoids the task state-machine complexity for client-rejected calls.
func scopeRejection(scope string) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		var text string
		if scope == "" {
			text = "This skill is not currently available on this server."
		} else {
			text = fmt.Sprintf("Insufficient scope: %s is required for this skill.", scope)
		}
		yield(a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(text)), nil)
	}
}

func (*eldamoAgentExecutor) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {}
}

// runEcho is the fallback executor: echoes the inbound text back.
func runEcho(_ context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		user := "unknown"
		if execCtx.User != nil {
			user = execCtx.User.Name
		}
		var b strings.Builder
		if execCtx.Message != nil {
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
		log.Printf("[A2A] echo user=%q text=%q", user, text)
		reply := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("Echo from Eldamo: "+text))
		yield(reply, nil)
	}
}

// mithlondOAuthSchemeName is the key used for the OAuth2 security scheme in the
// AgentCard. Clients discover authorize/token endpoints and required scopes from it.
const mithlondOAuthSchemeName a2a.SecuritySchemeName = "mithlond-oauth"

// allScopes is the canonical scope vocabulary for this server.
// Populated once here so the AgentCard and eldamo-admin stay in sync.
var allScopes = map[string]string{
	"lexicon:read":        "Search and read the Eldamo Tolkien lexicon",
	"audio:generate":      "Synthesize Elvish pronunciation audio via the TTS proxy",
	"agent:invoke":        "Send messages to the Eldamo A2A agent",
	"skill:name-generate": "Use the Elvish name-generation skill",
	"skill:translate":     "Use the Elvish translation skill",
	"skill:neologism":     "Use the Elvish neologism-builder skill",
}

// buildAgentCard constructs the public AgentCard for the given absolute base URL
// (e.g. "https://host"). The JSON-RPC interface URL and the OAuth token endpoint
// are both derived from the base URL; the authorize endpoint lives on the
// Firebase-hosted consent SPA and is always the same fixed URL.
func buildAgentCard(baseURL string) *a2a.AgentCard {
	return &a2a.AgentCard{
		Name:        "Eldamo Elvish Agent",
		Description: "Agentic access to Paul Strack's Eldamo Tolkien-language lexicon: search, derivations, name-generation, and (forthcoming) translation skills.",
		Version:     "0.4.0",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(baseURL+a2aBasePath, a2a.TransportProtocolJSONRPC),
		},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Capabilities:       a2a.AgentCapabilities{Streaming: true},

		// SecuritySchemes declares *how* to authenticate.
		// Clients read this to discover the OAuth endpoints and available scopes.
		SecuritySchemes: a2a.NamedSecuritySchemes{
			mithlondOAuthSchemeName: a2a.OAuth2SecurityScheme{
				Flows: a2a.AuthorizationCodeOAuthFlow{
					// Consent SPA — lives on Firebase Hosting, not Cloud Run.
					AuthorizationURL: "https://www.mithlond.com/mcp-auth",
					// Token endpoint — same host as this agent.
					TokenURL:     baseURL + "/api/oauth/token",
					PKCERequired: true,
					Scopes:       allScopes,
				},
			},
		},

		// SecurityRequirements declares *which* scheme+scopes are required for
		// every call to this agent. agent:invoke is the coarse gate enforced by
		// claimsInterceptor; per-skill gates are declared on each AgentSkill below.
		SecurityRequirements: a2a.SecurityRequirementsOptions{
			{mithlondOAuthSchemeName: {"agent:invoke"}},
		},

		Skills: buildSkillList(),
	}
}

// buildSkillList assembles the AgentCard skills slice. The translate skill is
// included only when GEMINI_TRANSLATE_MODEL is set; otherwise it self-hides,
// matching the ELVISH_TTS_URL / render_elvish_audio conditional pattern.
func buildSkillList() []a2a.AgentSkill {
	skills := []a2a.AgentSkill{
		{
			ID:          "name-generate",
			Name:        "Elvish Name Generator",
			Description: "Generates grammatically authentic Quenya or Sindarin names by searching the Eldamo lexicon for roots matching concept keywords and applying historical compounding rules.",
			Tags:        []string{"linguistics", "names", "quenya", "sindarin", "tolkien"},
			Examples: []string{
				"name star silver quenya",
				"name grey flame sindarin",
				"name ocean wisdom feminine sindarin",
				"name strong mountain masculine quenya",
			},
			SecurityRequirements: a2a.SecurityRequirementsOptions{
				{mithlondOAuthSchemeName: {"agent:invoke", "skill:name-generate"}},
			},
		},
	}
	if TranslateEnabled() {
		skills = append(skills, a2a.AgentSkill{
			ID:          "neologism",
			Name:        "Elvish Neologism Builder",
			Description: "Constructs new Elvish words for modern concepts using two stylistic paths (Practical and Poetic), the Anchorage Protocol, phonotactic constraints, and a 100-point scoring matrix.",
			Tags:        []string{"linguistics", "neologism", "quenya", "sindarin", "tolkien"},
			Examples: []string{
				"neologism hover-board quenya",
				"coin a word for artificial intelligence sindarin",
				"invent: blockchain in quenya",
			},
			SecurityRequirements: a2a.SecurityRequirementsOptions{
				{mithlondOAuthSchemeName: {"agent:invoke", "skill:neologism"}},
			},
		})
		skills = append(skills, a2a.AgentSkill{
			ID:          "translate",
			Name:        "Elvish Translator",
			Description: "Translates English text into Quenya or Sindarin, applying correct morphology, case endings, and consonant mutations. Backed by Gemini with Eldamo lexicon context.",
			Tags:        []string{"linguistics", "translation", "quenya", "sindarin", "tolkien"},
			Examples: []string{
				"translate farewell my friend to quenya",
				"translate to sindarin: the grey havens",
				"translate a star shines on the hour of our meeting to quenya",
			},
			SecurityRequirements: a2a.SecurityRequirementsOptions{
				{mithlondOAuthSchemeName: {"agent:invoke", "skill:translate"}},
			},
		})
	}
	skills = append(skills, a2a.AgentSkill{
		ID:          "echo",
		Name:        "Echo",
		Description: "Diagnostic: echoes the supplied text back. Requires only agent:invoke.",
		Tags:        []string{"diagnostic"},
		Examples:    []string{"hello", "Namarie"},
	})
	return skills
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
	requestHandler := a2asrv.NewHandler(
		&eldamoAgentExecutor{},
		a2asrv.WithCallInterceptors(&claimsInterceptor{}),
	)
	return a2asrv.NewJSONRPCHandler(requestHandler)
}
