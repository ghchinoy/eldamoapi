# 🌟 Eldamo Agent Tools 🌟

An high-performance **Model Context Protocol (MCP) Server** written in Go, providing AI agents with immediate, structured, linguistic access to Paul Strack's [Eldamo](http://eldamo.org/) Tolkien language lexicon compilation.

This repository bundles both the **MCP Server** itself and a set of **Linguistic Agent Skills**, which provide AI agents with advanced tools for Tolkien linguistic tasks (translation, name generation, and neologism composition).

It features a secure, modern (2026-standard) **OAuth 2.1 Authentication Layer** utilizing **Client ID Metadata Documents (CIMD)**, **Firebase Auth**, and **GCP Cloud Run**.

---

## 📖 Table of Contents
1. [Exposed MCP Tools](#-exposed-mcp-tools)
2. [Linguistic Agent Skills](#-linguistic-agent-skills)
3. [One-Line Installation (Prebuilt Binary)](#-one-line-installation-prebuilt-binary)
4. [Client Configurations](#-client-configurations)
5. [System Architecture & Deployment Overview](#-system-architecture--deployment-overview)
6. [Environment Variables & Configuration](#-environment-variables--configuration)
7. [Cloud Run Deployment](#-cloud-run-deployment)
8. [Guide: How to Build Your Own Go MCP Server](docs/how-to-create-mcp-server-go.md)
9. [Contributing & Development](docs/DEVELOPMENT.md)

---

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


## 🧠 Linguistic Agent Skills

This repository bundles specialized linguistic agent skills within the `skills/` directory, assisting agents to approach complex, artistic, and precise Elvish linguistic tasks:

### 1. `neologism-builder`
* **TL;DR:** Guides the creation of Neo-Elvish vocabulary. Offers a choice between **Practical (Functional)** compounding and **Poetic (Metaphorical)** concepts, evaluated via a **100-point Quantitative Scoring Matrix** that balances strict phonetic constraints against acoustic iconicity and proper-noun lineage, derived in part from The Digital Tolkien Project's [arda](https://github.com/digitaltolkien/arda) pronunciation library.

### 2. `tolkien-name-generator`
* **TL;DR:** Autonomously generates grammatically and historically-based Tolkien Elvish names for people, places, stars, or weapons. Compounds linguistic roots using proper Sandhi consonant merges and applies attested suffix paradigms.

### 3. `tolkien-translation`
* **TL;DR:** Translates English phrases into Tolkien's main languages (Quenya, Sindarin, and Adûnaic). Analyzes sentence grammar, verb conjugations, adjective agreements, and case morphology, ensuring appropriate historical dialect selection.

---

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

---

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
      "url": "https://eldamo-mcp-server-308690897031.us-central1.run.app/sse",
      "enabled": true,
      "oauth": {
        "clientId": "https://www.mithlond.com/metadata.json",
        "authorizationUrl": "https://www.mithlond.com/mcp-auth",
        "tokenUrl": "https://eldamo-mcp-server-308690897031.us-central1.run.app/api/oauth/token"
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
      "url": "https://eldamo-mcp-server-308690897031.us-central1.run.app/sse",
      "enabled": true,
      "oauth": {
        "clientId": "https://www.mithlond.com/metadata.json",
        "authorizationUrl": "https://www.mithlond.com/mcp-auth",
        "tokenUrl": "https://eldamo-mcp-server-308690897031.us-central1.run.app/api/oauth/token"
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
* **Gzip Embed Engine (`go:embed`):** Paul Strack's complete 24.8MB flat XML lexicon is preprocessed and embedded directly inside the statically compiled Go binary as a highly compressed gzip dataset (~4.5MB).
* **Double-Index Search Engine:** On startup, the server decompresses the dataset in under **20ms** and constructs in-memory prefix tries and inverted keyword indexes, allowing sub-millisecond search query latencies.
* **Low-Footprint Serverless Deployment:** The entire active runtime (tries, indices, and streamable multiplexers) consumes only **~40-50MB of RAM**, allowing us to deploy to cheap Google Cloud Run container instances.
* **Stateless Security Gateway:** Authentication is anchored on **OAuth 2.1** and **Client ID Metadata Documents (CIMD)**, issuing signed stateless JWTs (`MITHLOND_ACCESS_TOKEN`). No database checks are executed during active tool queries.

For a deep technical dive into these patterns, our Firestore schemas, the loopback-agnostic callback matching (RFC 8252), or our infrastructure hardening blueprints, see the [Detailed Architecture & Design Notes](docs/architecture.md).

---

## ⚙️ Environment Variables & Configuration

Local configurations are managed in a **`.env`** file at the project root. This file is excluded from Git to protect sensitive credentials.

The backend uses the following environment variables, evaluated with fallback/precedence logic:

| Variable Name | Purpose | fallback / Precedence Logic |
| :--- | :--- | :--- |
| `GCP_PROJECT` | Google Cloud project ID. | Sourced from `.env`; defaults to `testingproject-19c4c` during build. |
| `GCP_REGION` | Cloud Run container region. | Sourced from `.env`; defaults to `us-central1`. |
| `SERVICE_NAME`| Cloud Run deployment name. | Sourced from `.env`; defaults to `eldamo-mcp-server`. |
| `FIREBASE_PROJECT_ID` | Project ID for Firebase Admin verification. | Matches `GCP_PROJECT`. Defaults to `testingproject-19c4c`. |
| `FIREBASE_DATABASE` | Targets specific Firestore DB instance. | Sourced from `.env`; defaults to **`mithlond-services`** (NOT `(default)`). |
| `JWT_SIGNING_KEY` | Cryptographic key to sign/verify stateless tokens. | Sourced from `.env`; **dynamically generated as a random 32-char hex string** on first deploy if missing. |

---

## 🚀 Cloud Run Deployment

Deployment is fully automated using our secure shell pipeline. This pipeline loads your local `.env` variables, ensures GCP resources are fully initialized, and compiles the service in-cloud.

```bash
# Execute deployment pipeline
./scripts/deploy.sh
```

For detailed deployment blueprints and IAM safety configurations, see our [Architecture Documentation](docs/architecture.md).

---

## 🤝 Contributing & Development

We welcome contributions to the Eldamo MCP Server and Linguistic Agent Skills! If you want to build the server from source, run the integration test suites, configure lint analyzers, or tag a new release, see our [Developer Guide](docs/DEVELOPMENT.md).
