# Developer Guide: Build, Test, and Release

This document is the comprehensive manual for developers and contributors compiling the **Eldamo MCP Server** from source, configuring local environments, running integration test suites, and publishing official releases.

---

## 🏗️ Workspace & Environment Configurations

When compiling from source, the server initializes Firebase Auth and Google Cloud Firestore clients on startup. You **must** configure your local environment variables before starting the server.

### 1. Local Environment Variables (`.env`)
Create a **`.env`** file at the project root. This file is excluded from Git to protect sensitive developer credentials.

The backend evaluates the following variables on startup:

| Variable Name | Purpose | Fallback / Precedence Logic |
| :--- | :--- | :--- |
| `GCP_PROJECT` | Google Cloud project ID. | Sourced from `.env`; defaults to `testingproject-19c4c` during build. |
| `GCP_REGION` | Cloud Run container region. | Sourced from `.env`; defaults to `us-central1`. |
| `SERVICE_NAME`| Cloud Run deployment name. | Sourced from `.env`; defaults to `eldamo-mcp-server`. |
| `FIREBASE_PROJECT_ID` | Project ID for Firebase Admin verification. | Matches `GCP_PROJECT`. Defaults to `testingproject-19c4c`. |
| `FIREBASE_DATABASE` | Targets specific Firestore DB instance. | Sourced from `.env`; defaults to **`mithlond-services`** (NOT `(default)`). |
| `JWT_SIGNING_KEY` | Cryptographic key to sign/verify stateless tokens. | Sourced from `.env`; **dynamically generated as a random 32-char hex string** on first deploy if missing. |
| `ELVISH_TTS_URL` | (Optional) TTS Synthesizer URL. Production: `https://lhongant.mithlond.com`. Local dev: `http://localhost:8080`. | Set to enable the conditional `render_elvish_audio` MCP tool and A2A audio artifacts. |
| `GEMINI_TRANSLATE_MODEL` | (Optional) Gemini model for translate and neologism A2A skills. | Skills self-hide when unset. Example: `gemini-3.1-flash-lite`. |
| `GEMINI_LOCATION` | Vertex AI API location for Gemini skills. **Separate from `GCP_REGION`** (the Cloud Run deploy region). Newer Gemini models (3.x) require `global`; regional endpoints serve older generations. | Defaults to `global`. |
| `LOCAL_LLM_BASE_URL` | (Optional) Base URL of a local OpenAI-compatible LLM server (llama.cpp's `llama-server`, or mlx-lm's `mlx_lm.server`) for translate/neologism, e.g. `http://localhost:8080`. | **Takes precedence over `GEMINI_TRANSLATE_MODEL` when both are set.** Skills self-hide when neither is set. |
| `LOCAL_LLM_MODEL` | (Optional) Model identifier sent in each request body. | Defaults to `local-model`. Mostly informational/logging — `llama-server`/`mlx_lm.server` each serve one model per process, so this does not select weights. |
| `LOCAL_LLM_RUNTIME` | (Optional) Label recorded against usage records to distinguish which local runtime produced a response. | Defaults to `local`. Suggested values: `llama.cpp`, `mlx`. |


## 💻 Local Compilation & Development

Use our project-standard `Makefile` targets to build, compile, and run your local environment.

### 1. Build and Compile from Source
```bash
# Compile and build statically linked binaries for both the server and the admin CLI
make build

# Start the server locally
make run
```
*The compiled binaries will be output to `./bin/eldamoapi` and `./bin/eldamo-admin` respectively.*

### 2. Quick Start (Auth Bypass Mode)
For rapid local testing without needing to set up active user sessions or authenticate via Firebase, set the `AUTH_BYPASS` environment variable to `true`:
```bash
# Compiles and starts the server with AUTH_BYPASS=true
make run-dev
```
When `AUTH_BYPASS=true` is present, the `oauthMiddleware` will skip JWT token validation for all incoming requests, allowing your local agent client to connect directly without providing a valid `MITHLOND_ACCESS_TOKEN`.

> [!WARNING]
> Do not use `AUTH_BYPASS=true` in production or on any publicly accessible instance.

### 3. Enabling Audio Pronunciation (TTS Proxy)
The `render_elvish_audio` MCP tool is conditionally enabled. To use it, you must have a G2P/TTS service (such as `pronouncing-elvish`) running and set the `ELVISH_TTS_URL` environment variable.

1. Ensure your TTS backend is running (e.g., on port 8082).
2. Start the server:
   ```bash
   export ELVISH_TTS_URL=http://127.0.0.1:8082
   go run main.go oauth.go user.go
   ```
The `render_elvish_audio` tool will be automatically detected and available to `opencode`.

### 4. Using a Local LLM Backend for translate/neologism (llama.cpp or mlx_lm.server)

By default, the `translate` and `neologism` A2A skills call Vertex AI Gemini
(`GEMINI_TRANSLATE_MODEL`). For local testing — e.g. against a local Gemma 4
GGUF or MLX model, or a fine-tuned variant — point the server at a local
OpenAI-compatible server instead via `LOCAL_LLM_BASE_URL`. Both runtimes below
expose the same `/v1/chat/completions` streaming protocol, so no other config
differs between them beyond the port you run them on.

> See **[local-llm-servers.md](local-llm-servers.md)** for the full
> reference: install steps, runtime comparison, and detailed write-ups of
> gotchas only briefly noted below (the `mlx_lm.server` model-field
> requirement, reasoning-mode token starvation, and an IPv6 dial pitfall).

**Option A: llama.cpp (`llama-server`), serving a GGUF model:**
```bash
llama-server -m /path/to/model.gguf --port 8080
```

**Option B: mlx-lm (`mlx_lm.server`), serving an MLX model (Apple Silicon acceleration):**
```bash
mlx_lm.server --model /path/to/mlx-model-dir --port 8081
```
(Note: `mlx_lm.generate` is a one-shot CLI, not a server — use `mlx_lm.server`.)

Then start eldamo-server pointed at whichever is running:
```bash
export LOCAL_LLM_BASE_URL=http://localhost:8080   # or :8081 for mlx_lm.server
export LOCAL_LLM_MODEL=default_model               # REQUIRED as-is for mlx_lm.server (see warning below); any value works for llama-server
export LOCAL_LLM_RUNTIME=llama.cpp                 # or "mlx"; tags usage records
make run-dev
```

> [!NOTE]
> `llama-server` and `mlx_lm.server` each load **one model per process**.
> Switching between models (e.g. stock Gemma 4 vs. a fine-tuned variant, or
> comparing GGUF vs. MLX) means restarting the relevant server with a
> different `-m`/`--model` flag, or running separate instances on separate
> ports and changing `LOCAL_LLM_BASE_URL`.

> [!NOTE]
> `LOCAL_LLM_BASE_URL` takes precedence over `GEMINI_TRANSLATE_MODEL` when
> both are set — useful for flipping between local and hosted backends
> without editing your `.env`.

> [!WARNING]
> **`mlx_lm.server` requires `LOCAL_LLM_MODEL=default_model`.** Unlike
> `llama-server` (which serves one model and ignores the request's `model`
> field), `mlx_lm.server` treats any other value as a Hugging Face repo ID to
> fetch, and will fail with a 404 for an arbitrary `LOCAL_LLM_MODEL`.

> [!WARNING]
> **Reasoning-enabled Gemma checkpoints can be very slow or return empty
> responses against `mlx_lm.server`** for the translate/neologism skills'
> large system instructions: the model's chain-of-thought is streamed as a
> separate `reasoning` delta field (not part of the OpenAI spec) *before* any
> `content` delta, and can consume the entire `max_tokens` budget before
> producing an answer (`finish_reason: "length"`, empty response). Mitigate
> with `mlx_lm.server --chat-template-args '{"enable_thinking": false}'` at
> server startup, which disables the reasoning trace entirely and reduces
> generation time from minutes to seconds for these skills. `LOCAL_LLM_MAX_TOKENS`
> (default `4096`, vs. `mlx_lm.server`'s own default of `512`) can also be
> raised further if you keep reasoning enabled.

## ⚡ Local Client Configurations

When running the compiled server locally on port `8080` (utilizing `AUTH_BYPASS=true`), you can point your local agents directly to your loopback address.

### A. opencode Local Config
Add this to your local workspace `opencode.json` or global `~/.config/opencode/opencode.json`:
```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "eldamo-local": {
      "type": "remote",
      "url": "http://127.0.0.1:8080/sse",
      "enabled": true
    }
  }
}
```

### B. Claude Desktop Local Config
Add this to your `claude_desktop_config.json` file:
```json
{
  "mcpServers": {
    "eldamo-local": {
      "type": "remote",
      "url": "http://127.0.0.1:8080/sse"
    }
  }
}
```

## 🌐 Remote MCP Client Configurations (Production — OAuth 2.1)

These configs point at the live production server (`https://candir.mithlond.com`)
and exercise the full OAuth 2.1 / PKCE flow (CIMD client-id-as-URL and RFC 7591
DCR), not `AUTH_BYPASS`. Status reflects hands-on testing as of the l04.x/n3l
epics; update this table as clients change.

| Client | Status | Notes |
| :--- | :--- | :--- |
| **opencode** | ✅ Known-good | Daily-driver client for this project; zero known issues. |
| **Claude Desktop / Claude Code** | ⚠️ Unverified | Config should work per RFC 7591 DCR compliance (`oauth_dcr_test.go`), but no human has completed a live browser-consent handshake with it yet. |
| **Antigravity Desktop** | ⚠️ Partial | Browser-authorize flow works; a client-side race firing two concurrent `/token` requests on code-paste can produce a spurious `Unauthorized` on the *first* attempt (retry succeeds). Client-side bug, reported upstream. |
| **Antigravity CLI (`agy`)** | 🔴 Not recommended yet | Reference config only — see known issues below before relying on this for real work. |

### A. opencode Remote Config
Add this to your workspace `opencode.json` or global `~/.config/opencode/opencode.json`:
```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "eldamo-remote": {
      "type": "remote",
      "url": "https://candir.mithlond.com/sse",
      "enabled": true,
      "oauth": {
        "clientId": "https://www.mithlond.com/metadata.json",
        "authorizationUrl": "https://www.mithlond.com/mcp-auth",
        "tokenUrl": "https://candir.mithlond.com/api/oauth/token"
      }
    }
  }
}
```

### B. Claude Desktop / Claude Code Remote Config (unverified)
Claude's MCP client is expected to self-register via our RFC 7591 Dynamic
Client Registration endpoint (`registration_endpoint` in the RFC 8414
discovery document) rather than needing a static `clientId` — so, unlike
opencode/Antigravity above, no CIMD `clientId` should be required:
```json
{
  "mcpServers": {
    "eldamo-remote": {
      "type": "remote",
      "url": "https://candir.mithlond.com/sse"
    }
  }
}
```
> **Status:** Our server-side DCR flow is unit-tested (`oauth_dcr_test.go`),
> but this config has not yet been exercised end-to-end against a live
> Claude client. Treat as a starting point, not a confirmed-working recipe,
> until someone completes the browser-consent handshake and updates this note.

### C. Antigravity Desktop Remote Config (partial)
```json
{
  "mcpServers": {
    "eldamo-server": {
      "serverUrl": "https://candir.mithlond.com/sse",
      "headers": {
        "X-Mcp-Force-Sse": "true"
      },
      "oauth": {
        "enabled": true,
        "clientId": "https://www.mithlond.com/metadata.json",
        "authorizationUrl": "https://www.mithlond.com/mcp-auth",
        "tokenUrl": "https://candir.mithlond.com/api/oauth/token"
      }
    }
  }
}
```
`X-Mcp-Force-Sse: true` is **our own custom, non-standard header**
(`main.go`'s `McpMultiplexerHandler`), added specifically to route
Antigravity Desktop's long-lived `GET /sse` onto the legacy SSE transport,
which is unaffected by Cloud Run/GFE response buffering on that transport
(see bd `eldamo-server-1c7`). No other client needs or should send this
header — see the CLI notes below for why it actively causes problems there.

> **Known issue (client-side):** Antigravity's OAuth-code input handler can
> fire two concurrent `/api/oauth/token` requests ~35–80ms apart when a code
> is pasted. If the first (malformed/incomplete) request's `400` arrives
> before the second (valid) request's `200`, Antigravity surfaces a spurious
> `Unauthorized` and aborts even though authentication actually succeeded.
> Reported upstream to the Antigravity team; re-authorizing typically works
> on retry.

### D. Antigravity CLI (`agy`) Remote Config — reference only, known issues
This is the config `agy` itself ends up running with (captured from
`~/.gemini/config/mcp_config.json`); it is **not** a config you should expect
to hand-edit durably — see the first bullet below.

```json
{
  "mcpServers": {
    "eldamo-server": {
      "serverUrl": "https://candir.mithlond.com/sse",
      "oauth": {
        "clientId": "https://www.mithlond.com/metadata.json"
      }
    }
  }
}
```
(`agy` derives `authorizationUrl`/`tokenUrl` itself via `.well-known`
discovery rather than trusting config-supplied values — omitting them here is
expected, not a mistake.)

> **Known issues (all client-side, reported upstream, no server-side
> workaround exists):**
> 1. **Config does not persist edits.** `agy` silently rewrites
>    `~/.gemini/config/mcp_config.json` from its own internal state on every
>    launch — including re-adding `X-Mcp-Force-Sse: true` (see #3 below) even
>    after it's manually removed from the file. Edits must go through
>    Antigravity's GUI "MCP Store → View raw config" panel, if at all.
> 2. **`no pending auth state for server <name>`** on code paste. Root chain:
>    code exchange succeeds → the immediate post-auth `initialize` fails (see
>    #3) → user re-pastes a fresh code → `agy` has already consumed/discarded
>    its one-shot pending-OAuth-state object and never re-arms one for the
>    retry.
> 3. **Simultaneous legacy-SSE + Streamable-HTTP usage → intermittent
>    `session not found` on `tools/call`.** `agy` persistently sends
>    `X-Mcp-Force-Sse: true` (see item C above) on `GET /sse`, opening a
>    session on the legacy SSE handler, while its actual tool-call traffic
>    (`POST`/`DELETE /sse`) runs over the unrelated Streamable HTTP handler —
>    two disjoint session tables for one logical connection. No MCP server
>    can reconcile a client mixing both transports for a single session; `agy`
>    should not send this header at all, since all of its real traffic already
>    works correctly over plain Streamable HTTP without it.
>
> Two friction reports covering all of the above (with full log evidence and
> repro steps) have been sent to the Antigravity team. This project's own
> `main.go`/`oauth.go` were independently hardened during this investigation
> (GET-only scoping of `X-Mcp-Force-Sse`; full diagnostic logging on every
> `/api/oauth/token` validation branch) — those were genuine bugs on our side
> and are already fixed and deployed, but items 1–3 above remain open on the
> `agy` side.

## 🤖 A2A Protocol Testing

### A. A2A Remote Testing (production — candir.mithlond.com)

The production A2A agent is available at `https://candir.mithlond.com/a2a`
(`candir` = Sindarin "herald-man", from `cáno` √KAN + `-dîr`).

```bash
# Always export .env so JWT_SIGNING_KEY reaches child processes
set -a; source .env; set +a

a2acli discover --service-url https://candir.mithlond.com
a2acli send "name star silver quenya" \
  --service-url https://candir.mithlond.com --transport jsonrpc --wait \
  --token "$(make token)"
```

DNS: `candir.mithlond.com` CNAME → `ghs.googlehosted.com`
Domain mapping: `gcloud run domain-mappings describe --domain candir.mithlond.com --region us-central1`

### B. A2A Local Testing (a2acli)

The same binary also serves the A2A protocol at `/a2a` with the AgentCard at
`/.well-known/agent-card.json`. Test it with
[`a2acli`](https://github.com/ghchinoy/a2acli):

```bash
# Plumbing check (server started with make run-dev / AUTH_BYPASS=true)
a2acli discover --service-url http://127.0.0.1:8080
a2acli send "elen sila" --service-url http://127.0.0.1:8080 --transport jsonrpc --wait

# Auth-enforced (server started with make run)
a2acli send "Namarie" --service-url http://127.0.0.1:8080 \
  --transport jsonrpc --wait --token "$(make token)"
```

> `a2acli`'s default streaming mode opens a TUI and needs a TTY; use `--wait`
> (blocking) or `--immediate` in non-interactive shells / CI.

See the full [Test Plan](test-plan.md) and
[Dual-Protocol Architecture](dual-protocol-architecture.md) for details.

#### Generating dev JWTs (`make token`)

`make token` mints a 1-hour HS256 access token signed with `JWT_SIGNING_KEY`
(falls back to the dev key). The UID defaults to `dev-user`; override it:

```bash
make token              # UID=dev-user
make token UID=alice    # custom subject
```

This token works for **both** `/sse` (MCP) and `/a2a` (A2A) since they share the
same `oauthMiddleware`.

---

## 🗄️ Firestore Collections & One-Time Setup

The server uses the **`mithlond-services`** named database (not `(default)`). Three
collections exist; two require one-time infrastructure setup.

### Collection inventory

| Collection | Purpose | Created by | Visible in console? |
| :--- | :--- | :--- | :--- |
| `authorized_users` | OAuth user authorization records | `eldamo-admin add/pre-register` | Always (has permanent documents) |
| `mcp_auth_codes` | 5-minute PKCE authorization codes | OAuth callback handler | Only during an active OAuth flow; disappears when empty |
| `a2a_tasks` | A2A task state, history, and artifacts | First A2A request after deploy | Only when ≥ 1 document exists (see below) |

### Why `a2a_tasks` appears lazily

Firestore collections only show up in the Firebase console when they contain at
least one document. The `a2a_tasks` collection is new as of l04.5 and will be
invisible until the first A2A request reaches the production server. The
`go test -v -run "TestTaskstore"` integration tests create and then *delete*
their documents via `t.Cleanup`, so the collection will appear empty (and may
vanish from the console view) after the test suite finishes.

**To verify the collection exists:** send a **skill** request through `a2acli`
against the deployed server, then refresh the Firestore console.

> **Important:** Use a skill (`name-generate`, `translate`, `neologism`), **not**
> a plain message like `"Namarie"`. The `echo` fallback returns a bare `*Message`
> and bypasses the task state machine — it creates no Firestore document and
> returns an empty `Task ID:`. Only the three skills call `NewSubmittedTask`,
> write to `a2a_tasks`, and return a real Task ID.

```bash
set -a; source .env; set +a
a2acli send "name star quenya" \
  --service-url https://candir.mithlond.com \
  --transport jsonrpc --wait --token "$(make token)"
# -> Task ID: 019f...   ← non-empty; refresh Firestore console to see the document
```

### One-time: composite index for `a2a_tasks`

Required for the `List` method (`WHERE user ORDER BY updatedAt`). Created once;
idempotent. Index state can be checked with the second command.

```bash
gcloud firestore indexes composite create \
  --project=testingproject-19c4c \
  --database=mithlond-services \
  --collection-group=a2a_tasks \
  --field-config=field-path=user,order=ascending \
  --field-config=field-path=updatedAt,order=descending

# Check status (wait for READY before running taskstore tests):
gcloud firestore indexes composite list \
  --project=testingproject-19c4c \
  --database=mithlond-services \
  --quiet | grep a2a_tasks
```

### One-time: TTL policy for `a2a_tasks`

The `expiresAt` field is written on every task document (7 days from creation/update),
but Firestore only acts on it once a TTL policy is registered for that field.
Without this step, old task documents accumulate indefinitely.

```bash
gcloud firestore fields ttls update expiresAt \
  --collection-group=a2a_tasks \
  --enable-ttl \
  --database=mithlond-services \
  --project=testingproject-19c4c
```

TTL deletion is best-effort and typically runs within 24 hours of `expiresAt`.
This command is idempotent — safe to re-run.

> Both the index and TTL commands are also documented as comments in
> `scripts/deploy.sh` for reference during infrastructure setup.

---

## 🧪 Testing and Static Analysis

Always run the test suite and static code linter to verify your changes before creating a pull request or pushing tags.

### 1. Run Test Suite
Our comprehensive test suite validates database models, prefix/keyword indexers, SSRF dialer blocking, CIMD parser mocks, and cryptographic JWT verifications:
```bash
make test
```

### 2. Static Code Analysis (Linter)
Validate code quality using golangci-lint. Maintain a strict **0 issues** bar before merging or deploying:
```bash
golangci-lint run
```


## 🚀 Release & Versioning Procedures

We automate platform-specific builds using GoReleaser and tag-triggered GitHub Actions.

### 1. Automated Releases (GoReleaser)
We use [GoReleaser](https://goreleaser.com/) to automate the creation of platform-specific binaries for macOS, Linux, and Windows. Releases are triggered by **Git Tags**.

#### How to Release
1.  **Tag the release:**
    ```bash
    git tag -a v0.1.6 -m "Release v0.1.6"
    git push origin v0.1.6
    ```
2.  The GitHub Action (`.github/workflows/release.yml`) will automatically detect the tag, build the binaries, create a GitHub Release, and upload the artifacts.

### 2. Versioning Strategy
We follow **Semantic Versioning (SemVer)**. The version appears in the A2A
AgentCard and is the primary signal to clients about what the server can do.

| Bump | When | Examples |
| :--- | :--- | :--- |
| **Major** `x.0.0` | Breaking changes to the MCP tool API or A2A protocol surface | Removing an MCP tool, changing tool argument schema |
| **Minor** `0.x.0` | New user-facing capabilities — linguistic skills **or** A2A protocol features | New skill (name-generate, translate, neologism); new RPC capability (extendedAgentCard); new OAuth scope vocabulary |
| **Patch** `0.0.x` | Bug fixes, internal refactors, documentation, performance | Lint fixes, description updates, embed path changes |

**Guideline:** if a client reading the AgentCard needs to know about the change
(new capability advertised, new skill listed, new RPC available), it's a minor
bump. If the AgentCard is unchanged and behaviour is unchanged, it's a patch.

### 3. One-Line Installation (For Users)
Users can install the latest binary automatically using our shell script:
```bash
curl -sL https://raw.githubusercontent.com/ghchinoy/eldamoapi/main/scripts/install.sh | bash
```
