# 🌟 Eldamo Agent Tools 🌟

A high-performance, **dual-protocol agent server** written in Go, providing AI agents with immediate, structured, linguistic access to Paul Strack's [Eldamo](http://eldamo.org/) Tolkien language lexicon compilation. A single binary speaks both:

* **MCP** (Model Context Protocol) — the **transactional** surface: stateless tool calls (search, lookups, derivations, TTS) at `/sse`.
* **A2A** (Agent2Agent) — the **interactional** surface: agentic *skills* (name generation, translation, neologisms) at `/a2a`.

Both protocols are mounted on the same `net/http` mux, gated by the **same OAuth 2.1 JWT layer**, and read the **same in-memory lexicon** loaded once at startup. The mental model: *MCP exposes the tools; A2A exposes the agent that uses them.*

The server is designed for portability and serverless agility: it can be run locally as a native desktop service via either the prebuilt binaries or built from source, or deployed securely as a multi-user, OAuth-gated remote server on Google Cloud Run.

This repository bundles the **MCP Server**, the (in-progress) **A2A agent surface**, and a set of **Linguistic Agent Skills** for Tolkien linguistic tasks (translation, name generation, and neologism composition).

It features a secure, modern (2026-standard) **OAuth 2.1 Authentication Layer** utilizing **Client ID Metadata Documents (CIMD)**, **Firebase Auth**, and **GCP Cloud Run**.

---

## 📖 Table of Contents
1. [Exposed MCP Tools](#-exposed-mcp-tools)
2. [A2A Agent Surface](#-a2a-agent-surface)
3. [Linguistic Agent Skills](#-linguistic-agent-skills)
4. [One-Line Installation (Prebuilt Binary)](#-one-line-installation-prebuilt-binary)
5. [Client Configurations](#-client-configurations)
6. [System Architecture & Deployment Overview](#-system-architecture--deployment-overview)
7. [Environment Variables & Configuration](#-environment-variables--configuration)
8. [Cloud Run Deployment](#-cloud-run-deployment)
9. [Guide: How to Build Your Own Go MCP Server](docs/how-to-create-mcp-server-go.md)
10. [Contributing & Development](docs/DEVELOPMENT.md)


## 🛠️ Exposed MCP Tools

The server registers four specialized tools conforming to the Model Context Protocol specification:

### 1. `enquire_lexicon`
Performs general-purpose Tolkien linguistic search.
* **Arguments:**
  * `query` (string, required): Spelling prefix or search keyword (e.g. `"elen"`, `"flower"`, `"star"`).
  * `language` (string, optional): ISO/Eldamo language code filter (e.g. `"q"` for Quenya, `"s"` for Sindarin, `"pc"` for Primitive Elvish).
  * `speech` (string, optional): Part-of-speech filter (e.g. `"noun"`, `"verb"`, `"adjective"`, `"proper-name"`).
  * `category` (string, optional): Filter by lexicon category/era (e.g. `"neo"` for neologisms, `"primary"` for Tolkien's writings, `"root"` for roots).

### 2. `get_word_details`
Fetches complete linguistic metadata, historical notes, inflections, and semantic details for an individual entry.
* **Arguments:**
  * `id` (string, required): The unique Eldamo `page-id` (e.g., `"218765"`).

### 3. `get_derivations`
Explores the genealogical relationship and linguistic evolution of words in Tolkien's tongues.
* **Arguments:**
  * `id` (string, required): The unique Eldamo `page-id` (e.g., `"218765"`).
  * `direction` (string, optional): Either `"descendants"` (words produced by this word, default) or `"ancestors"` (the roots this word was derived from).

### 4. `get_root_anchors`
Retrieves proper names (characters, places, stars, weapons, etc.) recursively derived from a specific root or base word ID.
* **Arguments:**
  * `id` (string, required): The unique Eldamo `page-id` of the root or base word (e.g., `"2071154627"`).


## 🤝 A2A Agent Surface

Alongside MCP, the server exposes an **A2A (Agent2Agent)** endpoint so other agents can delegate *tasks* (not just call tools) to the Eldamo agent. This is the **interactional** counterpart to MCP's transactional tools.

* **AgentCard (public discovery):** `GET /.well-known/agent-card.json`
* **Protocol endpoint (auth-gated):** `POST /a2a` (JSON-RPC; SSE for streaming)
* **Auth:** the **same** Bearer JWT used for MCP. A2A clients pass it as `Authorization: Bearer …`.

> **Status:** All four A2A skills are live: `name-generate` (deterministic), `neologism` (LLM, two-path), `translate` (LLM, streaming), and `echo` (diagnostic). The agent is served at `https://candir.mithlond.com/a2a`.

### Quick test with [a2acli](https://github.com/ghchinoy/a2acli)

```bash
# Discover the agent (public, no auth)
a2acli discover --service-url https://candir.mithlond.com

# Token — always set -a first so JWT_SIGNING_KEY reaches child processes
set -a; source .env; set +a

# Name generation (deterministic)
a2acli send "name star silver quenya" \
  --service-url https://candir.mithlond.com --transport jsonrpc --wait \
  --token "$(make token)"

# Translation (LLM-backed, streaming)
a2acli send "translate farewell my friend to quenya" \
  --service-url https://candir.mithlond.com --transport jsonrpc --wait \
  --token "$(make token)"

# Neologism (LLM, two artifacts)
a2acli send "neologism hover-board quenya" \
  --service-url https://candir.mithlond.com --transport jsonrpc --wait \
  --token "$(make token)"
```

Full procedures and the auth matrix are in the [Test Plan](docs/test-plan.md).


## 🧠 Linguistic Agent Skills

This repository bundles specialized linguistic agent skills within the `skills/` directory, assisting agents to approach complex, artistic, and precise Elvish linguistic tasks:

### 1. `neologism-builder`
* **TL;DR:** Guides the creation of Neo-Elvish vocabulary. Offers a choice between **Practical (Functional)** compounding and **Poetic (Metaphorical)** concepts, evaluated via a **100-point Quantitative Scoring Matrix** that balances strict phonetic constraints against acoustic iconicity and proper-noun lineage, derived in part from The Digital Tolkien Project's [arda](https://github.com/digitaltolkien/arda) pronunciation library.

### 2. `tolkien-name-generator`
* **TL;DR:** Autonomously generates grammatically and historically-based Tolkien Elvish names for people, places, stars, or weapons. Compounds linguistic roots using proper Sandhi consonant merges and applies attested suffix paradigms.

### 3. `tolkien-translation`
* **TL;DR:** Translates English phrases into Tolkien's main languages (Quenya, Sindarin, and Adûnaic). Analyzes sentence grammar, verb conjugations, adjective agreements, and case morphology, ensuring appropriate historical dialect selection.


## 💾 One-Line Installation (Prebuilt Binary)

For users who do not wish to clone the repository or compile from source, you can install the latest prebuilt `eldamoapi` binary automatically to `/usr/local/bin`:
```bash
curl -sL https://raw.githubusercontent.com/ghchinoy/eldamoapi/main/scripts/install.sh | bash
```

Once installed, you can configure your local agent to run this binary as a local MCP server.

### A. Configure for opencode
Add this to your global `~/.config/opencode/opencode.json` or local workspace `opencode.json`:
```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "eldamo-local": {
      "type": "local",
      "command": ["eldamoapi"],
      "environment": {
        "AUTH_BYPASS": "true"
      },
      "enabled": true
    }
  }
}
```

### B. Configure for Claude Desktop
Add this to your `claude_desktop_config.json` file:
```json
{
  "mcpServers": {
    "eldamo-local": {
      "command": "eldamoapi",
      "env": {
        "AUTH_BYPASS": "true"
      }
    }
  }
}
```


## ⚡ Client Configurations

To add your remote, secure Eldamo MCP server to your desktop agent, follow the configurations below. 

> [!NOTE]
> Detailed information regarding the OAuth 2.1 & CIMD security flow, and our Firestore-based access control, can be found in our [Architecture Documentation](docs/architecture.md).

### 1. opencode
Configure the server in your local or global `opencode.json` file:

#### Project-Specific Config (Local)
Create an **`opencode.json`** file in the root of your local workspace directory:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "skills": {
    "paths": ["skills"]
  },
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

> [!TIP]
> The `"skills": { "paths": ["skills"] }` block is **essential** to enable OpenCode to discover and load the bundle of **Linguistic Agent Skills** (like the `neologism-builder`) included in the project directory.

#### Global Config (Universal)
Add the server block to your global configuration file at **`~/.config/opencode/opencode.json`**:

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

> [!IMPORTANT]
> Always quit and **restart opencode** after saving configuration changes for the remote MCP server to take effect.

---

## 🏗️ System Architecture & Deployment Overview

The Eldamo MCP Server is engineered for zero-dependency portability and stateless scale-to-zero serverless environments. 

### Key Architectural Pillars:
* **Dual-Protocol, Single Binary:** MCP (`/sse`, transactional tools) and A2A (`/a2a`, interactional skills) share one `net/http` mux, one OAuth gate, and one lexicon index — no second service to deploy. See [Dual-Protocol Architecture](docs/dual-protocol-architecture.md).
* **Gzip Embed Engine (`go:embed`):** Paul Strack's complete 24.8MB flat XML lexicon is preprocessed and embedded directly inside the statically compiled Go binary as a highly compressed gzip dataset (~4.5MB).
* **Double-Index Search Engine:** On startup, the server decompresses the dataset in under **20ms** and constructs in-memory prefix tries and inverted keyword indexes, allowing sub-millisecond search query latencies.
* **Low-Footprint Serverless Deployment:** The entire active runtime (tries, indices, and streamable multiplexers) consumes only **~40-50MB of RAM**, allowing us to deploy to cheap Google Cloud Run container instances.
* **Stateless Security Gateway:** Authentication is anchored on **OAuth 2.1** and **Client ID Metadata Documents (CIMD)**, issuing signed stateless JWTs (`MITHLOND_ACCESS_TOKEN`). No database checks are executed during active tool/skill queries.

For the dual-protocol design, see [Dual-Protocol Architecture](docs/dual-protocol-architecture.md). For a deep technical dive into the OAuth/CIMD patterns, Firestore schemas, the loopback-agnostic callback matching (RFC 8252), or infrastructure hardening blueprints, see the [Detailed Architecture & Design Notes](docs/architecture.md). For testing procedures, see the [Test Plan](docs/test-plan.md).


## ⚙️ Environment Variables & Configuration

Local configurations are managed in a **`.env`** file at the project root. This file is excluded from Git to protect sensitive credentials.

The backend uses the following environment variables, evaluated with fallback/precedence logic:

| Variable Name | Purpose | Fallback / Precedence Logic |
| :--- | :--- | :--- |
| `GCP_PROJECT` | Google Cloud project ID. | Sourced from `.env`; defaults to `testingproject-19c4c` during build. |
| `GCP_REGION` | Cloud Run **deployment** region (`gcloud run deploy --region`). | Sourced from `.env`; defaults to `us-central1`. Not passed to the container. |
| `SERVICE_NAME`| Cloud Run deployment name. | Sourced from `.env`; defaults to `eldamo-mcp-server`. |
| `FIREBASE_PROJECT_ID` | Project ID for Firebase Admin verification. | Matches `GCP_PROJECT`. Defaults to `testingproject-19c4c`. |
| `FIREBASE_DATABASE` | Targets specific Firestore DB instance. | Sourced from `.env`; defaults to **`mithlond-services`** (NOT `(default)`). |
| `JWT_SIGNING_KEY` | Cryptographic key to sign/verify stateless tokens. | Sourced from `.env`; **dynamically generated as a random 32-char hex string** on first deploy if missing. |
| `GEMINI_LOCATION` | Vertex AI **API location** for Gemini skills. Separate from `GCP_REGION` — newer models (gemini-3.x) require `global`; older models work with `us-central1`. | Defaults to `global`. |
| `GEMINI_TRANSLATE_MODEL` | Gemini model for the translate and neologism skills. When unset, both skills self-hide from the AgentCard and executor. | None — skills disabled if missing. |


## 🚀 Cloud Run Deployment

Deployment is fully automated using our secure shell pipeline. This pipeline loads your local `.env` variables, ensures GCP resources are fully initialized, and compiles the service in-cloud.

```bash
# Execute deployment pipeline
./scripts/deploy.sh
```

For detailed deployment blueprints and IAM safety configurations, see our [Architecture Documentation](docs/architecture.md).


## 🤝 Contributing & Development

We welcome contributions to the Eldamo MCP Server and Linguistic Agent Skills! If you want to build the server from source, run the integration test suites, configure lint analyzers, or tag a new release, see our [Developer Guide](docs/DEVELOPMENT.md).
