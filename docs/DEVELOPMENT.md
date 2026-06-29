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
| `ELVISH_TTS_URL` | (Optional) TTS Synthesizer URL. | Set to enable the conditional `render_elvish_audio` tool. |


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

### C. A2A Remote Testing (production — candir.mithlond.com)

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

### D. A2A Local Testing (a2acli)

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
We follow **Semantic Versioning (SemVer)**:
- **Major (x.0.0):** Breaking changes to the MCP tool API.
- **Minor (0.x.0):** New linguistic tools or agent skills.
- **Patch (0.0.x):** Bug fixes or performance improvements.

### 3. One-Line Installation (For Users)
Users can install the latest binary automatically using our shell script:
```bash
curl -sL https://raw.githubusercontent.com/ghchinoy/eldamoapi/main/scripts/install.sh | bash
```
