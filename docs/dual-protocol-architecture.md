# Dual-Protocol Architecture: MCP (Transactional) + A2A (Interactional)

This document describes how the Eldamo server exposes **two complementary agent
protocols from a single Go binary**, sharing one OAuth 2.1 security layer and one
in-memory lexicon index:

| Protocol | Nature | Surface | Who calls it |
| :--- | :--- | :--- | :--- |
| **MCP** (Model Context Protocol) | **Transactional** — stateless request/response tool calls | `/sse` (SSE + Streamable HTTP multiplexer) | An LLM that wants raw lexicon *tools* (search, lookups, derivations, TTS) |
| **A2A** (Agent2Agent) | **Interactional** — agentic *skills*, optionally streaming/long-running | `/a2a` (JSON-RPC) | Another agent that wants the Eldamo agent to *do a task* (generate a name, translate a phrase) |

The mental model: **MCP exposes tools; A2A exposes an agent that uses those
tools.** Both read the same lexicon; both are gated by the same JWT verifier.

![Dual-protocol architecture](dual-protocol-architecture.webp)

> Source: [`dual-protocol-architecture.dot`](dual-protocol-architecture.dot) ·
> regenerate with the [diagram workflow](#regenerating-diagrams).
> For the OAuth 2.1 / CIMD / Firestore flow in depth, see
> [`architecture.md`](architecture.md).

---

## 1. Design principles

1. **One binary, one mux, one OAuth layer.** Both protocol handlers are mounted
   on the same `http.ServeMux` in `main.go`, each wrapped by the *same*
   `oauthMiddleware`. There is no second service to deploy, no duplicated auth,
   and the ~4.5 MB embedded lexicon is decompressed and indexed **once**.
2. **Shared core, thin protocol adapters.** The business logic lives in the
   `index` package (`index.Index`). MCP tool handlers and the A2A
   `AgentExecutor` are both thin adapters over the same `lexiconIndex` global.
3. **Pure `net/http`.** Both the MCP Go SDK and `a2a-go` return plain
   `http.Handler`s, so they compose with the existing middleware chain without a
   web framework.
4. **Public discovery, protected protocol.** Discovery documents
   (`/.well-known/*`) are unauthenticated; the protocol endpoints they advertise
   require a Bearer JWT. The discovery chain follows RFC 9728 → RFC 8414: clients
   first probe `/.well-known/oauth-protected-resource` (PRM), which points to the
   authorization server at `/.well-known/oauth-authorization-server`.
5. **Deterministic-first skills.** A2A skills start as pure-Go logic over the
   lexicon (no LLM dependency). LLM-backed skills (e.g. free translation) are an
   opt-in fast-follow, not a baseline requirement.

---

## 2. Component map

| Concern | File / symbol |
| :--- | :--- |
| Server bootstrap, mux, routes | `main.go` → `main()` |
| MCP server + tool registration | `main.go` (`mcp.NewServer`, `mcp.AddTool`) |
| MCP transport multiplexer | `main.go` → `McpMultiplexerHandler` (SSE + Streamable HTTP) |
| RFC 9728 Protected Resource Metadata | `main.go` → `handleProtectedResourceMetadata` |
| RFC 8414 Authorization Server Metadata | `main.go` → `handleOAuthDiscovery` |
| A2A echo executor | `a2a.go` → `echoAgentExecutor` |
| A2A AgentCard builder | `a2a.go` → `buildAgentCard`, `handleAgentCard` |
| A2A JSON-RPC handler | `a2a.go` → `newA2AHandler` (`a2asrv.NewJSONRPCHandler`) |
| Shared security gate | `oauth.go` → `oauthMiddleware` |
| Client identity resolution (CIMD + DCR) | `oauth.go` → `resolveClient`, `FetchAndValidateCIMD`, `getRegisteredClient` |
| RFC 7591 Dynamic Client Registration | `oauth.go` → `handleClientRegistration` |
| OAuth token exchange / callback | `oauth.go` → `handleTokenExchange`, `handleAuthCallback` |
| Lexicon index (shared core) | `index/index.go` → `index.Index` |
| Admin / token CLI | `cmd/eldamo-admin` |

### Routes

| Path | Auth | Purpose |
| :--- | :--- | :--- |
| `GET /healthz` | none | Liveness probe |
| `GET /.well-known/oauth-authorization-server` | none | RFC 8414 — OAuth authorization server metadata |
| `GET /.well-known/oauth-protected-resource` | none | RFC 9728 — Protected Resource Metadata (bare) |
| `GET /.well-known/oauth-protected-resource/*` | none | RFC 9728 — path-suffixed variants (e.g. `/sse`, `/a2a`) |
| `GET /.well-known/agent-card.json` | none | A2A AgentCard discovery |
| `POST /api/oauth/register` | none | RFC 7591 DCR — register a public client, receive `client_id` |
| `POST /api/oauth/authorize-callback` | Firebase ID token | Consent-SPA callback → issues auth code |
| `POST /api/oauth/token` | PKCE / refresh | Token exchange |
| `* /` (exact root) | Bearer JWT | **MCP** — convenience alias for base-URL probes |
| `* /sse` | Bearer JWT | **MCP** (transactional) — SSE + Streamable HTTP multiplexed |
| `* /a2a`, `/a2a/` | Bearer JWT | **A2A** (interactional) |

---

## 3. Security model

A2A reuses the MCP security stack rather than introducing a parallel one.

### 3.1 Shared verifier

`oauthMiddleware` (`oauth.go`) wraps **both** `/sse` and `/a2a`. It:

- accepts `AUTH_BYPASS=true` for local dev;
- extracts the token from `Authorization: Bearer …` (A2A clients map
  `ServiceParams["authorization"]` → this header) with a `?token=` query
  fallback for SSE;
- validates the HMAC-SHA256 signature locally (no DB lookup) and requires
  `type == "access"`.

This means a single JWT issued by the OAuth flow — or by
`eldamo-admin token <uid>` / `make token` — works for **both** protocols.

### 3.2 AgentCard advertises the requirement

The A2A AgentCard (`buildAgentCard`) is the discovery contract. Today it
declares the JSON-RPC interface and skills. **Phase 4** adds an
`OAuth2SecurityScheme` (authorizationCode + PKCE) pointing at the existing
`https://www.mithlond.com/mcp-auth` authorize endpoint and `/api/oauth/token`,
so spec-compliant A2A clients can discover and satisfy the auth requirement.

### 3.3 Per-skill scope gating (Phase 2)

The existing `gate()` / `authorizeScopes` helpers and the scope vocabulary
(`lexicon:read`, `audio:generate`) extend naturally to A2A. The plan:

1. `oauthMiddleware` stashes the validated `jwt.MapClaims` into the request
   `context` (benefits MCP `gate()` too).
2. The A2A transport already threads the request context into
   `ExecutorContext`, and an `a2asrv.CallInterceptor` (registered via
   `WithCallInterceptors`) reads those claims to populate `a2asrv.User` and
   reject calls lacking the required scope (e.g. `skill:name-generate`).

### 3.4 Client auth ergonomics

Client identity acquisition is a **pluggable front door**: two onboarding mechanisms
converge on the same `authorization_code + PKCE` core and produce the same JWT.

- **CIMD clients** (opencode) set `client_id` to the HTTPS URL of a hosted metadata
  document (`https://www.mithlond.com/metadata.json`). The server fetches it via an
  SSRF-safe client to read `redirect_uris`. No pre-registration needed.

- **DCR clients** (Gemini Spark, most standard OAuth clients) POST their metadata to
  `POST /api/oauth/register` and receive an opaque `client_id` (prefix `mcp-client-`).
  Spark shows this as "automatic registration" in its Connected Apps UI. No client
  secret is issued — public/PKCE clients only.

- **Pre-registered / manual clients** (admin-issued tokens, A2A via `a2acli`) obtain
  a JWT out-of-band via `make token` and pass `--token`. Use `set -a; source .env; set +a`
  so `JWT_SIGNING_KEY` is inherited by child processes; plain `source .env` only sets a
  shell variable and the fallback dev key will be rejected by Cloud Run.

---

## 4. The interactional layer (A2A) in detail

### 4.1 AgentExecutor

The single interface an implementer satisfies is `a2asrv.AgentExecutor`
(`Execute` / `Cancel`), using Go 1.23+ iterators (`iter.Seq2[a2a.Event, error]`)
to *yield* events. The SDK consumes them, persists task state, and streams to the
client. Phase 1 ships `echoAgentExecutor`, which echoes the inbound message text.

### 4.2 Skills

A2A `AgentSkill`s are **declarative metadata** advertised in the AgentCard; the
SDK does not dispatch by skill — routing is the executor's job. The three
existing repo skills map directly onto A2A skills:

| Go skill executor (`internal/skills/`) | A2A skill ID | Execution model |
| :--- | :--- | :--- |
| `internal/skills/name_generate.go` | `name-generate` | Deterministic Go over the lexicon; no LLM dependency |
| `internal/skills/neologism.go` | `neologism` | LLM-backed (streaming, two artifacts: Practical + Poetic Path) |
| `internal/skills/translate.go` | `translate` | LLM-backed (streaming) |

All three skills self-hide on the AgentCard when no LLM backend is configured
(`neologism` and `translate`), or are always visible (`name-generate`).
LLM backend is selected at startup via env vars — see
[DEVELOPMENT.md](DEVELOPMENT.md) for the precedence rules (`LOCAL_LLM_BASE_URL`
takes priority over `GEMINI_TRANSLATE_MODEL`). A fourth skill, `echo`
(diagnostic fallback), is always present.

### 4.3 Transport choice: JSON-RPC

`/a2a` uses `a2asrv.NewJSONRPCHandler` — a single POST endpoint that namespaces
cleanly under a path prefix and uses SSE for streaming methods (the existing
`sseLoggingMiddleware` already sets `X-Accel-Buffering: no` to defeat Cloud
Run/GFE buffering). REST was avoided because its internal `/v1/...` routes want a
root mount; gRPC was avoided because it needs a separate listener and cannot
serve the well-known card.

### 4.4 Task store

The default in-memory task store is fine for a single instance. Because Cloud Run
autoscales, resumable/long-running tasks need a shared store; a **Firestore-backed
`taskstore`** (reusing the existing `mithlond-services` database) is the
production path (Phase 5).

---

## 5. Phased roadmap

Tracked as `bd` issues under the **A2A exposure** epic (`bd list`).

| Phase | Goal | Status |
| :--- | :--- | :--- |
| **1. Wiring spike** | Mount A2A JSON-RPC + AgentCard behind `oauthMiddleware`; echo executor; verify with `a2acli` | ✅ Done |
| **2. Claims + scope gating** | Stash claims in context; `CallInterceptor` for `User`/scopes; wire MCP `gate()` | ✅ Done |
| **3. First real skill** | `name-generate` (deterministic) over the lexicon, streaming progress | ✅ Done |
| **4. AgentCard security + scopes** | `OAuth2SecurityScheme` in card; new scopes in `eldamo-admin` | ✅ Done |
| **5. Production hardening** | Firestore `taskstore`, conformance tests, deploy/env, docs | ✅ Done |

Local LLM support (Gemma 4 via llama.cpp / mlx_lm.server) and per-call
token/cost tracking are tracked separately under epic `eldamo-server-hk1`
(`bd show eldamo-server-hk1`).

---

## 6. Regenerating diagrams

Diagrams are authored in Graphviz DOT and rendered to WebP:

```bash
cd docs
dot -Tpng -Gdpi=144 dual-protocol-architecture.dot -o /tmp/dpa.png
cwebp -q 90 /tmp/dpa.png -o dual-protocol-architecture.webp
```

Requires `graphviz` (`dot`) and `cwebp` (from `webp`), both installable via
Homebrew: `brew install graphviz webp`.
