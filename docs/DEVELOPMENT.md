# Developer Guide: Build, Test, and Release

This document outlines the operational procedures for configuring your local development environment, compiling from source, testing features, and publishing official releases.

---

## 💻 Local Development Configurations

If you are developing features or bug fixes, you can build from source and run the server locally on port `8080` (utilizing your local `.env` configuration).

### Prerequisite: Setup Environment Variables
Because the server initializes Firebase and Firestore clients on startup, you **must** configure your local environment variables in a `.env` file first.
* Create a `.env` file at the project root (see `README.md` for variable mappings).
* Ensure your local terminal is authenticated with Google Cloud (Application Default Credentials).

### 1. Compile and Build
Use our project-standard `Makefile` targets to compile the server:
```bash
# Build statically-linked binaries for the server and the admin CLI to ./bin
make build

# Start the server locally
make run
```

### 2. Enabling Auth Bypass (Developer Mode)
For rapid local testing without needing to set up active user sessions or authentication, set the `AUTH_BYPASS` environment variable to `true` when starting the server.
```bash
# Bypasses oauthMiddleware signature checks locally
make run-dev
```
When `AUTH_BYPASS=true` is present, the `oauthMiddleware` will skip JWT token validation for all incoming requests, allowing your local `opencode` client to connect without providing a valid `MITHLOND_ACCESS_TOKEN`.

> [!WARNING]
> Do not use `AUTH_BYPASS=true` in production or on any publicly accessible instance.

### 3. Enabling Audio Pronunciation
The `render_elvish_audio` MCP tool is conditionally enabled. To use it, you must have a G2P/TTS service (such as `pronouncing-elvish`) running and set the `ELVISH_TTS_URL` environment variable.

1. Ensure your TTS backend is running (e.g., on port 8082).
2. Start the server:
   ```bash
   export ELVISH_TTS_URL=http://127.0.0.1:8082
   go run main.go oauth.go user.go
   ```
The `render_elvish_audio` tool will be automatically detected and available to `opencode`.

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

---

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
