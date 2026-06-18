# Local Development: Auth & Audio

For local development and testing, authentication can be bypassed and TTS audio synthesis can be enabled.

## Enabling Auth Bypass

Set the `AUTH_BYPASS` environment variable to `true` when starting the server.

```bash
export AUTH_BYPASS=true
# Optional: Set the TTS service URL if needed
export ELVISH_TTS_URL=http://127.0.0.1:8082
go run main.go oauth.go
```

When `AUTH_BYPASS=true` is present, the `oauthMiddleware` will skip JWT token validation for all incoming requests, allowing the `opencode` client to connect without providing a valid `MITHLOND_ACCESS_TOKEN`. 

**WARNING:** Do not use `AUTH_BYPASS=true` in production or on any publicly accessible instance.

## Enabling Audio Pronunciation

The `render_elvish_audio` MCP tool is conditionally enabled. To use it, you must have a G2P/TTS service (such as `pronouncing-elvish`) running and set the `ELVISH_TTS_URL` environment variable.

1. Ensure your TTS backend is running (e.g., on port 8082).
2. Start the server:
   ```bash
   export ELVISH_TTS_URL=http://127.0.0.1:8082
   go run main.go oauth.go
   ```
The `render_elvish_audio` tool will be automatically detected and available to `opencode`.
