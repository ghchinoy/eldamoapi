# Eldamo MCP Server: Architecture and Design Notes

This document provides an overview of the design, dataset preparation, and deployment strategy for hosting the Eldamo lexicon dataset as a secure, authenticated Model Context Protocol (MCP) Server on Google Cloud Run.


## Architecture Diagram

![Eldamo MCP Server Architecture](architecture.webp)


## 1. High-Level System Design

The system is split into three main parts:
1. **The Dataset Preparation Pipeline:** A data-ingestion pipeline that transforms Paul Strack's raw Eldamo XML lexicon into a flat, indexable gzipped JSON Lines (`eldamo.jsonl.gz`) format.
2. **The Go MCP Server:** A streamable HTTP multiplexer server written in Go that loads the lexicon, builds an in-memory search index, exposes the Model Context Protocol API over secure Server-Sent Events (SSE), and streams response results.
3. **The OAuth 2.1 & CIMD Security Gateway:** Implements a highly secure, zero-database-registration authorization layer using **Client ID Metadata Documents (CIMD)**, **Firebase Auth**, and **GCP Cloud Run / Cloud Firestore**.


## 2. Dataset Preparation & Storage Strategy

### Current Assets
- Raw data: From [eldamo](https://github.com/pfstrack/eldamo), located in `~/projects/eldamo-group/eldamo/src/data/eldamo-data.xml`.
- Parser utility: Located in `~/projects/eldamo-group/eldamo-parse/xml-to-jsonl`. It compiles the XML lexicon of 22,000+ entries into `eldamo.jsonl` (24MB) in sub-second time.

### Storage Decision: Embedded (`go:embed`)
To serve queries with the lowest possible latency and resource overhead, we have explicitly selected **Go Embedding (`go:embed`)** to load the preprocessed `eldamo.jsonl` file.

#### Rationale and Tradeoffs:
- **Fast Startup (Sub-100ms):** When Cloud Run scales from zero instances to one, the Go binary boots and parses the entire 24.8MB flat JSONL file in less than 100ms.
- **Minimal RAM Footprint (~40–50MB):** Holds all 22,000 parsed words, a prefix search trie, and an inverted keyword index in memory. This allows us to use the lowest Cloud Run tier (256MB RAM / 1 vCPU), making hosting inexpensive.
- **Zero External Network Dependencies:** Unlike a GCS-fetching model, the server does not need to perform any HTTP/GCS requests on startup, removing points of failure and networking overhead.
- **Stable Lexicon:** Given that the Eldamo lexicon is relatively stable and updated on a regular/periodic basis, the requirement to rebuild and redeploy the container when data changes is an acceptable tradeoff.


## 3. Go MCP Server Implementation Design

The Go application is developed using the official [`github.com/modelcontextprotocol/sdk-go`](https://github.com/modelcontextprotocol/sdk-go) library, which supports streamable HTTP transport out-of-the-box.

### In-Memory Search Index
At 24MB, the entire lexicon can be easily stored in memory (~22,000 entries). To ensure instant responses (sub-millisecond search latencies), the Go server constructs an in-memory index on startup:
- **Trie (Prefix Tree) Index:** For autocompletion and word matching (e.g. finding words beginning with `elen-`).
- **Inverted Index:** For keyword searching across glosses, neologism glosses, and notes (normalized and tokenized).
- **Language Map:** Quick lookup by Elvish language dialect (`q` for Quenya, `s` for Sindarin, `t` for Telerin, etc.).

### Exposed MCP Tools
The server publishes five specialized tools:
- `enquire_lexicon`: The primary tool for general exploration.
- `get_word_details`: Fetches full morphological detail, notes, and references for a specific page-ID.
- `get_derivations`: Lists words derived from this word, or the roots this word derived from.
- `get_root_anchors`: Retrieves proper names (characters, places) recursively derived from a root.
- `render_elvish_audio`: Synthesizes pronunciation via a TTS proxy (enabled by `ELVISH_TTS_URL`).

### Transport endpoints
The MCP multiplexer supports both SSE and Streamable HTTP transports and is mounted at two paths:

| Path | Description |
| :--- | :--- |
| `/sse` | Primary MCP endpoint (SSE + Streamable HTTP multiplexed) |
| `/` (exact root) | Convenience alias — allows clients to paste the bare domain URL |

Both require a valid Bearer JWT and share the same `oauthMiddleware → sseLogging → gate("lexicon:read")` chain.


## 4. OAuth 2.1 Security: Public Discovery + Multiple Client Onboarding Paths

The server implements the current MCP Authorization specification, which layers three RFCs to achieve both broad compatibility and zero-database convenience:

### 4.1 Public Discovery (RFC 9728 + RFC 8414)

The discovery chain a client walks before it can authenticate:

```
1. Client probes:  GET /.well-known/oauth-protected-resource[/<path>]
                   → RFC 9728 Protected Resource Metadata (PRM)
                   → declares which authorization_server protects this resource

2. Client fetches: GET /.well-known/oauth-authorization-server
                   → RFC 8414 Authorization Server Metadata
                   → provides token_endpoint, registration_endpoint, etc.

3. 401 challenge on /sse or /:
   WWW-Authenticate: Bearer resource_metadata="https://candir.mithlond.com/.well-known/oauth-protected-resource"
```

Both discovery endpoints are **public/unauthenticated**. The PRM also responds correctly to path-suffixed probes (`/.well-known/oauth-protected-resource/sse`, `.../a2a`) that RFC 9728 clients send to identify the specific resource they are accessing.

### 4.2 Client Onboarding: CIMD and DCR (pluggable front doors)

Client identity acquisition is a **pluggable front door** — two mechanisms are accepted, both converging on the same `authorization_code + PKCE` core:

| Mechanism | client_id shape | How it works | Typical client |
| :--- | :--- | :--- | :--- |
| **CIMD** (Client ID Metadata Document) | HTTPS URL (e.g. `https://www.mithlond.com/metadata.json`) | Server fetches the URL via an SSRF-safe client, reads `redirect_uris` and `client_name` dynamically. Trust via DNS ownership. | opencode, CIMD-aware agents |
| **DCR** (RFC 7591 Dynamic Client Registration) | Opaque string (prefix `mcp-client-`) | Client POSTs `redirect_uris` to `POST /api/oauth/register`; server issues a `client_id`, persists to Firestore `registered_clients`. | Gemini Spark, most standard OAuth clients |

The authorization-server metadata advertises both:
```json
{
  "registration_endpoint": "https://candir.mithlond.com/api/oauth/register",
  "client_id_metadata_document_supported": true
}
```

`resolveClient()` in `oauth.go` dispatches by `client_id` shape at the authorize step: URL → CIMD fetch; opaque → Firestore lookup. The token exchange (`handleTokenExchange`) and JWT issuance are identical regardless of onboarding path.

### 4.3 Firebase Web Authentication SPA (`mithlond-web`)

- Firebase Hosting hosts `public/mcp-auth.html`, the user consent SPA, at `https://www.mithlond.com/mcp-auth`.
- Firebase Hosting reverse-proxies `/api/oauth/**` to Cloud Run — this covers `/api/oauth/authorize-callback`, `/api/oauth/token`, and `/api/oauth/register`, eliminating CORS issues.
- The SPA handles both CIMD client_ids (URL-shaped, displays hostname) and DCR client_ids (opaque string, displays "Registered Application") in its consent UI.

### 4.4 Granular Tool Authorization (ACL Gating)
Beyond authentication, the server implements granular control:
- **`authorized_users` Collection:** Stores user-specific records in Firestore including `active` status, `roles`, and `scopes`.
- **JWT Embedding:** `handleTokenExchange` embeds the user's `scopes` and `roles` into JWT claims (`sub`, `scopes`, `roles`).
- **Middleware Gating:** `gate("lexicon:read", handler)` validates claims locally during tool execution — no Firestore lookup during active calls.

### 4.5 Stateless Signed Access Tokens (JWT)
- `authorization_code + PKCE S256` exchange at `/api/oauth/token` issues HMAC-SHA256 signed JWTs.
- `oauthMiddleware` validates the JWT signature and expiration **locally** during tool calls — the session validation loop is CPU-bound, sub-millisecond, and stateless.
- One token works for **both** `/sse` (MCP) and `/a2a` (A2A) — "one token, both protocols".

### 4.6 Firestore Collections

| Collection | Purpose | Notes |
| :--- | :--- | :--- |
| `authorized_users` | User authorization records (`active`, `scopes`, `roles`) | Managed via `eldamo-admin` CLI |
| `mcp_auth_codes` | Transient 5-minute PKCE auth codes | 5-min TTL policy; single-use, deleted on exchange |
| `registered_clients` | DCR-registered clients (`client_id` → `redirect_uris`, metadata) | Written by `/api/oauth/register`; read by `resolveClient()` |

All collections are in the `mithlond-services` Firestore database (not `(default)`).


## 5. Administrative Tooling Pattern

We implement administrative operations (adding/granting/revoking users) via a private, compiled CLI utility (`eldamo-admin`) rather than exposed API endpoints.
- **Why?** Exposing admin functions via HTTP endpoints increases the attack surface significantly. By using a private binary that reads credentials from the local environment, we ensure that only operators with direct infrastructure access can modify user permissions.

## 6. Deployment Blueprint & Security Hardening

* **IAM Least-Privilege Role Binding:** The deployment script automatically checks for a dedicated, isolated service account `eldamo-mcp-runner`. It binds only the **`roles/datastore.user`** permission to allow reading and purging transient Firestore codes, leaving other cloud segments fully protected.
* **Auto-Purging TTL Policy:** The database TTL is automatically configured via `gcloud` to purge expired codes from the `mcp_auth_codes` collection after 5 minutes, preventing manual maintenance tasks.
* **Docker Containerization:** Google Cloud Build runs a multi-stage compilation in-cloud, compiling an optimized, statically linked Go binary running inside a minimal Scratch container.
