package main

import (
	"context"
	"fmt"
	"iter"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/ghchinoy/eldamoapi/skills"
	"google.golang.org/genai"
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

// ── GenAI client (package main, shared across skills via Deps) ────────────────

var (
	genaiOnce   sync.Once
	genaiClient *genai.Client
	genaiErr    error
)

// initGenAIClient lazily initialises the shared Vertex AI client.
// Called when building Deps in newA2AHandler; nil is returned (and skills
// self-disable) when GEMINI_TRANSLATE_MODEL is unset.
func initGenAIClient() (*genai.Client, error) {
	genaiOnce.Do(func() {
		project := os.Getenv("GCP_PROJECT")
		location := os.Getenv("GEMINI_LOCATION")
		if location == "" {
			location = "global"
		}
		log.Printf("[A2A] Initializing Vertex AI client (project=%s, location=%s)", project, location)
		genaiClient, genaiErr = genai.NewClient(context.Background(), &genai.ClientConfig{
			Project:  project,
			Location: location,
			Backend:  genai.BackendVertexAI,
		})
		if genaiErr != nil {
			log.Printf("[A2A] Failed to create Vertex AI client: %v", genaiErr)
		}
	})
	return genaiClient, genaiErr
}

// translateEnabled reports whether the LLM-backed skills are configured.
func translateEnabled() bool {
	return os.Getenv("GEMINI_TRANSLATE_MODEL") != ""
}

// translateModelName returns the configured Gemini model name.
func translateModelName() string {
	if m := os.Getenv("GEMINI_TRANSLATE_MODEL"); m != "" {
		return m
	}
	return "gemini-3.1-flash-lite"
}

// buildDeps constructs the skills.Deps for the current process configuration.
// If translate/neologism are disabled (no model env var), GenAI is nil and
// those skills self-hide.
func buildDeps() *skills.Deps {
	d := &skills.Deps{
		Index:       lexiconIndex,
		TranslateMD: translateSkillMD,
		NeologismMD: neologismSkillMD,
		ModelName:   translateModelName(),
	}
	if translateEnabled() {
		client, err := initGenAIClient()
		if err != nil {
			log.Printf("[A2A] Vertex AI client unavailable; LLM skills disabled: %v", err)
		} else {
			d.GenAI = client
		}
	}
	return d
}

// ── Executor ──────────────────────────────────────────────────────────────────

// eldamoAgentExecutor dispatches to skill executors via injected Deps.
// Routing order matters: neologism/translate have specific prefixes and must
// be checked before name-generate which fires on any language keyword.
type eldamoAgentExecutor struct {
	deps *skills.Deps
}

var _ a2asrv.AgentExecutor = (*eldamoAgentExecutor)(nil)

func (e *eldamoAgentExecutor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	if skills.IsNeologismRequest(execCtx.Message) {
		if !e.deps.LLMEnabled() {
			return scopeRejection("")
		}
		if !hasScope(execCtx.User, "skill:neologism") {
			return scopeRejection("skill:neologism")
		}
		return skills.RunNeologism(ctx, execCtx, e.deps)
	}
	if skills.IsTranslateRequest(execCtx.Message) {
		if !e.deps.LLMEnabled() {
			return scopeRejection("")
		}
		if !hasScope(execCtx.User, "skill:translate") {
			return scopeRejection("skill:translate")
		}
		return skills.RunTranslate(ctx, execCtx, e.deps)
	}
	if skills.IsNameRequest(execCtx.Message) {
		if !hasScope(execCtx.User, "skill:name-generate") {
			return scopeRejection("skill:name-generate")
		}
		return skills.RunNameGenerate(ctx, execCtx, e.deps)
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

func (*eldamoAgentExecutor) Cancel(_ context.Context, _ *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
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
		Description: "Agentic access to Paul Strack's Eldamo Tolkien-language lexicon. Skills: Quenya/Sindarin name generation (deterministic, lexicon-grounded), morphologically-guided translation (Gemini, streaming), and dual-path neologism construction with phonotactic scoring (Gemini, two artifacts).",
		Version:     "0.4.1",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(baseURL+a2aBasePath, a2a.TransportProtocolJSONRPC),
		},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Capabilities:       a2a.AgentCapabilities{Streaming: true, ExtendedAgentCard: true},

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
	if translateEnabled() {
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

// buildExtendedAgentCard returns the richer AgentCard served to authenticated
// callers via the A2A GetExtendedAgentCard RPC. Differences from the public card:
//
//   - All four skills are always listed (translate and neologism included
//     regardless of GEMINI_TRANSLATE_MODEL, annotated when unavailable).
//   - Richer per-skill descriptions with concrete input examples.
//   - Provider and documentation URL populated.
func buildExtendedAgentCard(baseURL string) *a2a.AgentCard {
	llmAvailable := translateEnabled()

	translateDesc := "Translates English text into Quenya or Sindarin using Gemini, applying morphology, case endings, and consonant mutations. Phase 1 fetches Eldamo lexicon roots for grounded vocabulary."
	neologismDesc := "Constructs new Elvish words for modern concepts. Runs the Anchorage Protocol (GetRootAnchors) then generates two named artifacts: a Practical (functional) path and a Poetic (metaphorical) path, each with a 100-point phonotactic score."
	if !llmAvailable {
		translateDesc += " [GEMINI_TRANSLATE_MODEL not configured — skill unavailable on this instance]"
		neologismDesc += " [GEMINI_TRANSLATE_MODEL not configured — skill unavailable on this instance]"
	}

	return &a2a.AgentCard{
		Name:        "Eldamo Elvish Agent",
		Description: "Agentic access to Paul Strack's Eldamo Tolkien-language lexicon. Skills: Quenya/Sindarin name generation (deterministic, lexicon-grounded), morphologically-guided translation (Gemini, streaming), and dual-path neologism construction with phonotactic scoring (Gemini, two artifacts).",
		Version:     "0.4.1",
		Provider: &a2a.AgentProvider{
			Org: "Mithlond",
			URL: "https://www.mithlond.com",
		},
		DocumentationURL: "https://candir.mithlond.com/.well-known/agent-card.json",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(baseURL+a2aBasePath, a2a.TransportProtocolJSONRPC),
		},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Capabilities:       a2a.AgentCapabilities{Streaming: true, ExtendedAgentCard: true},
		SecuritySchemes: a2a.NamedSecuritySchemes{
			mithlondOAuthSchemeName: a2a.OAuth2SecurityScheme{
				Flows: a2a.AuthorizationCodeOAuthFlow{
					AuthorizationURL: "https://www.mithlond.com/mcp-auth",
					TokenURL:         baseURL + "/api/oauth/token",
					PKCERequired:     true,
					Scopes:           allScopes,
				},
			},
		},
		SecurityRequirements: a2a.SecurityRequirementsOptions{
			{mithlondOAuthSchemeName: {"agent:invoke"}},
		},
		Skills: []a2a.AgentSkill{
			{
				ID:   "name-generate",
				Name: "Elvish Name Generator",
				Description: "Generates grammatically authentic Quenya or Sindarin names. " +
					"Searches the Eldamo lexicon for roots matching concept keywords, applies " +
					"vowel elision and consonant assimilation rules, and appends an " +
					"attested suffix (-ndil, -wen, -on, -iel). Fully deterministic; no LLM.",
				Tags: []string{"linguistics", "names", "quenya", "sindarin", "tolkien"},
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
			{
				ID:          "translate",
				Name:        "Elvish Translator",
				Description: translateDesc,
				Tags:        []string{"linguistics", "translation", "quenya", "sindarin", "tolkien"},
				Examples: []string{
					"translate farewell my friend to quenya",
					"translate to sindarin: the grey havens",
					"translate a star shines on the hour of our meeting to quenya",
				},
				SecurityRequirements: a2a.SecurityRequirementsOptions{
					{mithlondOAuthSchemeName: {"agent:invoke", "skill:translate"}},
				},
			},
			{
				ID:          "neologism",
				Name:        "Elvish Neologism Builder",
				Description: neologismDesc,
				Tags:        []string{"linguistics", "neologism", "quenya", "sindarin", "tolkien"},
				Examples: []string{
					"neologism hover-board quenya",
					"coin a word for artificial intelligence sindarin",
					"invent: blockchain in quenya",
				},
				SecurityRequirements: a2a.SecurityRequirementsOptions{
					{mithlondOAuthSchemeName: {"agent:invoke", "skill:neologism"}},
				},
			},
			{
				ID:          "echo",
				Name:        "Echo",
				Description: "Diagnostic fallback: echoes the supplied text. Stateless — creates no task document, returns no Task ID. Use for connectivity checks only.",
				Tags:        []string{"diagnostic"},
				Examples:    []string{"hello", "Namarie"},
			},
		},
	}
}

// extendedCardProducer implements a2asrv.ExtendedAgentCardProducer.
// It reads the baseURL from the request context (stashed by oauthMiddleware)
// so the extended card's supportedInterfaces URL is always correct.
type extendedCardProducer struct{}

func (e *extendedCardProducer) ExtendedCard(ctx context.Context, _ *a2a.GetExtendedAgentCardRequest) (*a2a.AgentCard, error) {
	baseURL := BaseURLFromContext(ctx)
	if baseURL == "" {
		// Fallback for contexts where the URL wasn't stashed (shouldn't happen
		// in normal operation behind oauthMiddleware).
		baseURL = "https://candir.mithlond.com"
	}
	return buildExtendedAgentCard(baseURL), nil
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
//
// Task store selection:
//   - When firestoreClient is available (Cloud Run or local with Firebase
//     credentials), tasks are persisted in Firestore (a2a_tasks collection).
//     This enables get_task / subscribe / list_tasks to work across
//     Cloud Run instances and restarts.
//   - When firestoreClient is nil (AUTH_BYPASS local dev without credentials),
//     falls back to the in-memory store, which is correct for single-process
//     testing but loses tasks on restart.
func newA2AHandler() http.Handler {
	var store taskstore.Store
	if firestoreClient != nil {
		store = NewFirestoreTaskStore(firestoreClient, a2asrv.NewTaskStoreAuthenticator())
		log.Println("[A2A] Using Firestore task store (a2a_tasks collection)")
	} else {
		store = taskstore.NewInMemory(nil)
		log.Println("[A2A] Using in-memory task store (no Firestore client)")
	}

	requestHandler := a2asrv.NewHandler(
		&eldamoAgentExecutor{deps: buildDeps()},
		a2asrv.WithCallInterceptors(&claimsInterceptor{}),
		a2asrv.WithTaskStore(store),
		a2asrv.WithExtendedAgentCardProducer(&extendedCardProducer{}),
	)
	return a2asrv.NewJSONRPCHandler(requestHandler)
}
