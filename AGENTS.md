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
* **Firestore Target:** Transient 5-minute authorization codes are stored in the **`mithlond-services`** database instance (NOT the `(default)` instance) on GCP project `testingproject-19c4c` under collection `mcp_auth_codes`. An automated TTL policy is enabled on the `expires_at` field.

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

## 📋 Issue Tracking with Beads

This project uses **bd (beads)** for distributed, Git-integrated issue tracking. 
Run `bd prime` for full AI workflow context, or use these quick reference commands:

* **`bd ready`:** Find unblocked work.
* **`bd create "Title" --type task --priority 2`:** Create a new issue.
* **`bd close <id> --reason "..."`:** Complete and close an issue.
* **`bd dolt push`:** Push beads database commits to the remote.

Before executing a task, always claim it (`bd update <id> --claim --status=in_progress`) to maintain accurate workspace coordination.
