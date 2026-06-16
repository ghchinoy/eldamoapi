# Eldamo MCP Server: Architecture and Design Notes

This document provides a comprehensive overview of the design, dataset preparation, and deployment strategy for hosting the Eldamo lexicon dataset as a Streamable HTTP Model Context Protocol (MCP) Server on Google Cloud Run.

---

## Architecture Diagram

![Eldamo MCP Server Architecture](file:///Users/ghchinoy/projects/eldamo-server/docs/architecture.webp)

---

## 1. High-Level System Design

The system is split into three main parts:
1. **The Dataset Preparation Pipeline:** A data-ingestion pipeline that transforms Paul Strack's raw Eldamo XML lexicon into a flat, indexable JSON Lines (`eldamo.jsonl`) format.
2. **The Go MCP Server:** A streamable HTTP SSE server written in Go that loads the lexicon, builds an in-memory search index, exposes the Model Context Protocol API over HTTP, and streams response tokens to clients.
3. **The Deployment Infrastructure (Cloud Run):** Google Cloud Run hosts the containerized Go server serverlessly, scaling to zero when inactive and securing access through Google Cloud IAM permissions (leveraging local development proxies for client access).

---

## 2. Dataset Preparation & Storage Strategy

### Current Assets
- Raw data: Located in `~/projects/eldamo-group/eldamo/src/data/eldamo-data.xml`.
- Parser utility: Located in `~/projects/eldamo-group/eldamo-parse/xml-to-jsonl`. It compiles the XML lexicon of 22,000+ entries into `eldamo.jsonl` (24MB) in sub-second time.

### Storage Alternatives for the Go Server
To serve queries fast, the server needs access to the `eldamo.jsonl` file. We have three deployment storage models:

1. **Embedded (`go:embed`):**
   - **How it works:** We copy `eldamo.jsonl` into the Go codebase and use Go's `embed` package to pack it directly into the binary.
   - **Pros:** Zero-configuration, lightning-fast boot time, no runtime file dependencies, and highly portable.
   - **Cons:** Any lexicon update requires rebuilding the Docker image and redeploying. (Given the dataset is relatively stable, this is highly viable).

2. **Local Volume (Docker Image):**
   - **How it works:** We place `eldamo.jsonl` in a specific directory within the Docker image during construction.
   - **Pros:** Decoupled from Go source files (compiles slightly faster).
   - **Cons:** Still requires image rebuilding on dataset changes.

3. **Cloud Storage (GCS) Dynamic Sync:**
   - **How it works:** On startup, the Go application downloads `eldamo.jsonl` from a Google Cloud Storage bucket (`gs://eldamo-datasets/exports/eldamo.jsonl`).
   - **Pros:** Lexicon updates do not require redeploying code. We can simply write a GCS trigger to clear/re-load in-memory indices, or restart the container.
   - **Cons:** Adds a cold-start latency to Cloud Run (needs to download 24MB on startup), and introduces external cloud dependencies.

---

## 3. Go MCP Server Implementation Design

The Go application will be developed using the official [`github.com/modelcontextprotocol/sdk-go`](https://github.com/modelcontextprotocol/sdk-go) library, which supports streamable HTTP transport out-of-the-box.

### In-Memory Search Index
At 24MB, the entire lexicon can be easily stored in memory (~22,000 entries). To ensure instant responses (sub-millisecond search latencies), the Go server will construct an in-memory index on startup:
- **Trie (Prefix Tree) Index:** For autocompletion and word matching (e.g. finding words beginning with `elen-`).
- **Inverted Index:** For keyword searching across glosses, neologism glosses, and notes (normalized and tokenized).
- **Language Map:** Quick lookup by Elvish language dialect (`q` for Quenya, `s` for Sindarin, `t` for Telerin, etc.).

### Exposed MCP Tools
The server will publish the following tools to any connected AI agent:

- `enquire_lexicon`: The primary tool for general exploration.
  - **Arguments:**
    - `query` (string, required): Search keyword or partial word.
    - `language` (string, optional): Filter by language (e.g., `q`, `s`, `pc`, `on`).
    - `speech` (string, optional): Filter by part of speech (e.g., `noun`, `verb`, `adj`).
    - `category` (string, optional): Filter by era/category (e.g., `neo`, `primary`).
- `get_word_details`: Fetches full morphological detail, notes, and references for a specific page-ID.
  - **Arguments:**
    - `id` (string, required): Eldamo `page-id` (e.g., `218765`).
- `get_derivations`: Lists words derived from this word, or the roots this word derived from.
  - **Arguments:**
    - `id` (string, required): Eldamo `page-id`.
    - `direction` (enum, optional): `ancestors` (what it came from) or `descendants` (what it produced).

---

## 4. Cloud Run Transport & Authentication

Cloud Run excels at streamable HTTP transport via Server-Sent Events (SSE). 

### HTTP Endpoints
The Go server will expose:
1. `GET /sse`: The endpoint where clients connect to establish a Server-Sent Events stream. The server responds with a `transportId`.
2. `POST /messages`: The endpoint where clients send JSON-RPC requests, including the `transportId` headers to route the messages into the established SSE session.

### Authentication & Invocation Flow
To prevent public abuse and ensure secure operation, the Cloud Run service will be deployed with **unauthenticated access disabled**.
1. **Google Cloud IAM Protection:** Only clients with `roles/run.invoker` permission on the service can invoke it.
2. **Local Client Development / Tooling Connection:**
   Clients running on a local workstation (like your IDE or a CLI-based coding agent) will connect using the **gcloud run services proxy**:
   ```bash
   gcloud run services proxy eldamo-mcp-server --region us-central1 --port=3000
   ```
   This command starts a local HTTP server on `localhost:3000` which injects your local authenticated `gcloud` OIDC identity headers into all outgoing requests.
3. **Client Configuration:**
   The coding agent configures the MCP server using:
   ```json
   {
     "mcpServers": {
       "eldamo": {
         "url": "http://localhost:3000/sse"
       }
     }
   }
   ```

---

## 5. Work Breakdown and Task Plan

We will manage our plan using **beads (`bd`)** and run a step-by-step grilling session to finalize our decisions.

### Phase 1: Foundation and Dataset Preparation
- **Task 1:** Re-structure Go project workspace under `eldamo-server`.
- **Task 2:** Implement dataset injection mechanics (embedding vs GCS).
- **Task 3:** Write JSONL stream loader and parsing structures in Go.

### Phase 2: Search Indexing Engine
- **Task 4:** Build the in-memory inverted index and search trie.
- **Task 5:** Write unit tests to validate search capabilities (prefixes, exact match, gloss keyword search).

### Phase 3: Go MCP Server & Transport
- **Task 6:** Integrate `sdk-go` and register the MCP tools.
- **Task 7:** Implement SSE Transport server handlers (`/sse` and `/messages`).
- **Task 8:** Write integration tests verifying JSON-RPC SSE streaming.

### Phase 4: Cloud Run Dockerization & Deployment
- **Task 9:** Write a multi-stage `Dockerfile` and compile the service.
- **Task 10:** Create deployment scripts using `gcloud run deploy`.
- **Task 11:** Verify end-to-end connectivity using local client proxy.
