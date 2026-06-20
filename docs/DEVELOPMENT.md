# Local Development, Releases, and Versioning

This document outlines the operational procedures for configuring your local development environment, testing features, and publishing official releases.

---

## 💻 Local Development Configurations

For local development and testing, authentication can be bypassed and TTS audio synthesis can be enabled.

### 1. Enabling Auth Bypass
Set the `AUTH_BYPASS` environment variable to `true` when starting the server.
```bash
export AUTH_BYPASS=true
# Optional: Set the TTS service URL if needed
export ELVISH_TTS_URL=http://127.0.0.1:8082
go run main.go oauth.go user.go
```
When `AUTH_BYPASS=true` is present, the `oauthMiddleware` will skip JWT token validation for all incoming requests, allowing the `opencode` client to connect without providing a valid `MITHLOND_ACCESS_TOKEN`.

> [!WARNING]
> Do not use `AUTH_BYPASS=true` in production or on any publicly accessible instance.

### 2. Enabling Audio Pronunciation
The `render_elvish_audio` MCP tool is conditionally enabled. To use it, you must have a G2P/TTS service (such as `pronouncing-elvish`) running and set the `ELVISH_TTS_URL` environment variable.

1. Ensure your TTS backend is running (e.g., on port 8082).
2. Start the server:
   ```bash
   export ELVISH_TTS_URL=http://127.0.0.1:8082
   go run main.go oauth.go user.go
   ```
The `render_elvish_audio` tool will be automatically detected and available to `opencode`.

---

## 🚀 Release & Versioning Procedures

We automate platform-specific builds using GoReleaser and tag-triggered GitHub Actions.

### 1. Automated Releases (GoReleaser)
We use [GoReleaser](https://goreleaser.com/) to automate the creation of platform-specific binaries for macOS, Linux, and Windows. Releases are triggered by **Git Tags**.

#### How to Release
1.  **Tag the release:**
    ```bash
    git tag -a v0.1.4 -m "Release v0.1.4"
    git push origin v0.1.4
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
