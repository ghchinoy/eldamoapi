# 🏹 Eldamo MCP Server: Agent Onboarding Instructions

Welcome! This document provides critical architectural context, workspace layouts, and testing commands to help developer agents onboard and contribute to this repository with high precision.

---

## 🏗️ Workspace & Component Architecture

This project is a high-performance **Go-native Model Context Protocol (MCP) Server** with a zero-registration, stateless **OAuth 2.1 security layer**. It consists of two tightly coupled repositories:

1. **Backend Go Server (`eldamo-server` - This Repository):**
   * Exposes multiplexed SSE/Streamable HTTP MCP tools (`enquire_lexicon`, `get_word_details`, `get_derivations`, `render_elvish_audio`).
   * Loads Paul Strack's lexicon into memory from a compressed embedded filesystem (`data/eldamo.jsonl.gz`).
   * Handles OAuth 2.1 token exchanges (`/api/oauth/token`) and local JWT bearer verification (`oauthMiddleware`).
   * Includes a conditional TTS proxy capability (enabled via `ELVISH_TTS_URL`) to support audio pronunciation.
2. **Frontend UI (`mithlond-web` - Sibling Directory `../mithlond-web`):**
   * Hosted on **Firebase Hosting** (linked to custom domain `www.mithlond.com`).
   * Serves the consent single-page application at `/mcp-auth` (`public/mcp-auth.html`).
   * Proxies all `/api/oauth/**` endpoints back to Cloud Run via Hosting rewrite proxying to eliminate CORS.

---

## 🔒 Security & Database Model

* **Client ID Metadata Documents (CIMD):** We do not pre-register clients in a database. The `client_id` is an HTTPS URL of a JSON metadata file hosted on the client's domain. The backend fetches it on-the-fly via an SSRF-safe connection dialer.
* **Stateless Active Usage:** We issue stateless signed HMAC-SHA256 JWTs (`MITHLOND_ACCESS_TOKEN`). No database lookups are performed during active MCP tool execution.
* **Firestore Target:** Transient 5-minute authorization codes and user authorization records (`authorized_users`) are stored in the **`mithlond-services`** database instance (NOT the `(default)` instance) on GCP project `testingproject-19c4c` under collection `mcp_auth_codes` and `authorized_users`. An automated TTL policy is enabled on the `expires_at` field for codes.

---

## 💻 Developer Tooling Quick Reference

Use these `Makefile` targets to build, run, and test your changes:

* **`make run`:** Compiles and launches the Go server locally on port `8080`.
* **`make test`:** Runs the entire test suite, validating indexes, SSRF dialers, CIMD parsing, and middleware.
* **`make token`:** Generates a secure, 1-hour Access Token JWT using your local `.env` key for local or remote MCP testing.
* **`golangci-lint run`:** Runs the project-standard linter. Maintain a strict **0 issues** bar before merging or deploying.
* **`./scripts/deploy.sh`:** Builds and deploys the container to Cloud Run (automatically configures minimal service accounts and binds `roles/datastore.user`).

---

## 📚 Further Documentation

For detailed local setup, troubleshooting, and environment configuration (including authentication bypass and TTS configuration), see [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md).

---

## 🛡️ Security & Observability Protocol for Agents

When working on security-sensitive code (e.g., `oauth.go`, middleware, or deployment scripts), observe these practices:

*   **Log Exhaustively for Auth:** Always ensure new auth/middleware logic logs sufficient context for debugging (e.g., request headers, specific rejection reasons). Use `log.Printf` to surface rejection details to Cloud Run stderr.
*   **SSRF Protection:** All external URL fetches (CIMD discovery, etc.) **must** pass through `SafeHTTPClient()`. Never use standard `http.Get`.
*   **Stateless Debugging:** Since our tokens are stateless JWTs, do not attempt to look up tokens in Firestore during tool execution. Only perform signature validation locally.
*   **Infrastructure Caching:** If a remote test fails due to stale configuration, use the `CACHE_BUSTER=$(date +%s)` pattern in your deployment environment variables to force a fresh container build.
*   **CIMD Compliance:** Before implementing new client support, verify that metadata discovery signals capability (`client_id_metadata_document_supported: true`) and follows RFC 8252 (loopback port-agnostic matching).
*   **Admin CLI Tooling:** Use the `eldamo-admin` CLI tool (`cmd/eldamo-admin`) for all user-authorization and scope-management tasks. Do not expose administrative API endpoints in the main `eldamo-server`.

This project uses **bd (beads)** for distributed, Git-integrated issue tracking. 
Run `bd prime` for full AI workflow context, or use these quick reference commands:

* **`bd ready`:** Find unblocked work.
* **`bd create "Title" --type task --priority 2`:** Create a new issue.
* **`bd close <id> --reason "..."`:** Complete and close an issue.
* **`bd dolt push`:** Push beads database commits to the remote.

Before executing a task, always claim it (`bd update <id> --claim --status=in_progress`) to maintain accurate workspace coordination.

---

## 🧭 Build Design & Direction Hints

These are pragmatic conventions and gotchas learned while building this server.
They complement (do not duplicate) the deep docs in `docs/`
([dual-protocol-architecture](docs/dual-protocol-architecture.md),
[architecture](docs/architecture.md), [test-plan](docs/test-plan.md)).

### Architecture intent
* **Dual-protocol, one binary.** This is both an **MCP** server (transactional
  tools at `/sse`) *and* an **A2A** agent (interactional skills at `/a2a`). When
  adding a new protocol/surface, **mount it on the existing `http.ServeMux` in
  `main()` behind the existing `oauthMiddleware`** — do not spin up a second
  service or a second auth path.
* **Stay framework-free.** Both the MCP Go SDK and `a2a-go` expose plain
  `http.Handler`s. Keep using stdlib `net/http` + the existing middleware chain
  (`oauthMiddleware` → `sseLoggingMiddleware` → handler). Do not introduce
  gin/echo/chi.
* **Shared core, thin adapters.** All linguistic logic belongs in the `index`
  package (the single in-memory `lexiconIndex`). MCP tool handlers and the A2A
  `AgentExecutor` are thin adapters over it — never duplicate search/derivation
  logic into a protocol handler.
* **One token, both protocols.** A JWT from the OAuth flow (or `make token`)
  must work for `/sse` and `/a2a` alike. Scope vocabulary lives in JWT claims +
  `cmd/eldamo-admin`; reuse `gate()` / `authorizeScopes` rather than inventing a
  parallel check.
* **Skills are deterministic-first.** Prefer pure-Go skills over the lexicon.
  Make any LLM-backed skill optional and self-hiding when its backend env is
  unconfigured (mirror the `ELVISH_TTS_URL` conditional-registration pattern).
* **Public discovery, protected protocol.** `/.well-known/*` documents are
  unauthenticated; the endpoints they advertise are JWT-gated. Derive
  `scheme://host` for absolute URLs from the request (honor `X-Forwarded-Host`)
  as in `handleOAuthDiscovery` / `requestBaseURL`.

### Local dev & verification gotchas
* **Port 8080 is often occupied.** Run on an alternate port
  (`PORT=8099 go run .`) and clean up lingering background servers with
  `lsof -ti:8099 | xargs kill -9`. `go run .` children can outlive a killed
  parent.
* **`a2acli` is the A2A conformance client.** Validate the A2A surface with it
  on every change. Its default streaming mode opens a Bubble Tea TUI and needs a
  TTY — in non-interactive shells/CI use `--wait` or `--immediate`.
* **`make token`** mints a dev JWT (defaults `UID=dev-user`; override
  `make token UID=alice`). The signing key must match the server's
  `JWT_SIGNING_KEY` — always run `source .env` first, otherwise the
  fallback key `"temporary-dev-signing-key-mithlond"` is used and the
  token will be rejected by any non-dev server (Cloud Run, staging, etc.).
* **Trust the linter as a bug detector.** A `golangci-lint` `ineffassign`
  finding here surfaced a real auth bug (a shadowed `err` made pre-registered
  users 403). Investigate findings before silencing them; keep the **0-issue**
  bar.

### Documentation & diagrams
* **Diagrams: DOT → WebP.** Author Graphviz `.dot` in `docs/`, render with
  `dot -Tpng -Gdpi=144 X.dot -o /tmp/X.png && cwebp -q 90 /tmp/X.png -o docs/X.webp`
  (`brew install graphviz webp`). Note: `shape=actor` is unsupported in
  Graphviz 14 and silently falls back to `box`.
* **Keep the roadmap in `bd`.** Phase/epic structure for cross-cutting features
  lives in beads (e.g. the A2A epic `eldamo-server-l04`); reference bd IDs from
  docs rather than maintaining a separate status list.
