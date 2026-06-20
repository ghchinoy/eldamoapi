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
The server publishes three specialized tools:
- `enquire_lexicon`: The primary tool for general exploration.
- `get_word_details`: Fetches full morphological detail, notes, and references for a specific page-ID.
- `get_derivations`: Lists words derived from this word, or the roots this word derived from.



## 4. Secure OAuth 2.1 & CIMD Authentication

To allow decentralized, safe, and frictionless access for third-party AI agents without requiring manual API key distribution or exposing our database to registration spam, we utilize **OAuth 2.1 with Client ID Metadata Documents (CIMD)**:

### 1. Zero-Database Client Registration
- Instead of using a server-issued random string, the client's identity is an HTTPS URL controlled by the client application (e.g., `client_id = https://app.client.example/mcp-client-metadata.json`).
- During authorization, the Go server performs a secure, SSRF-resistant fetch to this URL to dynamically read client metadata (e.g., `redirect_uris`, `client_name`).
- Trust is established purely via DNS ownership and transport security.

### 2. Firebase Web Authentication SPA (`mithlond-web`)
- Reuses the existing **`mithlond-web`** Firebase Hosting project.
- Firebase Hosting hosts `public/mcp-auth.html` which handles user authentication popup flows using the Firebase Web SDK.
- To avoid Cross-Origin Resource Sharing (CORS) errors, Firebase Hosting reverse-proxies `/api/oauth/**` requests directly to the Cloud Run Go backend.

### 3. Granular Tool Authorization (ACL Gating)
Beyond simple authentication, we implement granular control:
- **`authorized_users` Collection:** Stores user-specific records in Firestore including `active` status, `roles`, and `scopes`.
- **JWT Embedding:** When the token issuer (`handleTokenExchange`) generates a custom JWT, it queries the user's `scopes` and `roles` from Firestore and embeds them into the JWT claims (`sub`, `scopes`, `roles`).
- **Middleware Gating:** The Go server enforces these permissions using a `gate` middleware. MCP handlers are wrapped: `mux.Handle("/sse", gate("lexicon:read", secureHandler))`. This validates the claims locally without DB hits during tool execution.

### 4. Stateless Signed Access Tokens (JWT)
- On code exchange at `/api/oauth/token` (validated via PKCE S256), the server issues stateless, HMAC-SHA256 signed JSON Web Tokens (JWT).
- During active tool calls at `/sse`, the Go server executes `oauthMiddleware` locally, validating the JWT signature and expiration **without querying Firestore during active tool calls**. This makes the entire session validation loop CPU-bound, sub-millisecond, and exceptionally scalable.

### 5. Transient Firestore Codes
- **Firestore Collection (`mcp_auth_codes`):** Setup on the dedicated `mithlond-services` database instance.
- Stores ONLY temporary 5-minute authorization codes during the token exchange handshake.
- A **Time-To-Live (TTL)** policy automatically purges codes from Firestore immediately upon expiration.


## 5. Administrative Tooling Pattern

We implement administrative operations (adding/granting/revoking users) via a private, compiled CLI utility (`eldamo-admin`) rather than exposed API endpoints.
- **Why?** Exposing admin functions via HTTP endpoints increases the attack surface significantly. By using a private binary that reads credentials from the local environment, we ensure that only operators with direct infrastructure access can modify user permissions.

## 6. Deployment Blueprint & Security Hardening

* **IAM Least-Privilege Role Binding:** The deployment script automatically checks for a dedicated, isolated service account `eldamo-mcp-runner`. It binds only the **`roles/datastore.user`** permission to allow reading and purging transient Firestore codes, leaving other cloud segments fully protected.
* **Auto-Purging TTL Policy:** The database TTL is automatically configured via `gcloud` to purge expired codes from the `mcp_auth_codes` collection after 5 minutes, preventing manual maintenance tasks.
* **Docker Containerization:** Google Cloud Build runs a multi-stage compilation in-cloud, compiling an optimized, statically linked Go binary running inside a minimal Scratch container.
