# Eldamo MCP Server Release Process

## 🚀 Automated Releases (Goreleaser)

We use [Goreleaser](https://goreleaser.com/) to automate the creation of platform-specific binaries for macOS, Linux, and Windows. Releases are triggered by **Git Tags**.

### How to Release
1.  **Tag the release:**
    ```bash
    git tag -a v1.0.0 -m "Release v1.0.0"
    git push origin v1.0.0
    ```
2.  The GitHub Action (`.github/workflows/release.yml`) will automatically detect the tag, build the binaries, create a GitHub Release, and upload the artifacts.

## 📦 Versioning Strategy
We follow **Semantic Versioning (SemVer)**:
- **Major (x.0.0):** Breaking changes to the MCP tool API.
- **Minor (0.x.0):** New linguistic tools or agent skills.
- **Patch (0.0.x):** Bug fixes or performance improvements.

## 🛠️ One-Line Installation (For Users)

Users can install the latest binary automatically using our shell script:

```bash
curl -sL https://raw.githubusercontent.com/ghchinoy/eldamoapi/main/scripts/install.sh | bash
```
