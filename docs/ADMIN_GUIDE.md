# 🛡️ Mithlond Eldamo MCP Server: Administrator's Guide

This document is the official operational guide for system administrators managing the **Mithlond Eldamo MCP Server** authentication gateway, user scopes, and diagnostic flows.

---

## 🏗️ Identity & Authorization Architecture

Our security pipeline separates **Identity** (who the user is, verified securely by Google/Firebase Auth) from **Authorization** (what resources/tools the user can access, managed dynamically in our Firestore database).

1.  **Identity Verification:** The desktop client or web frontend authenticates via Google.
2.  **Authorization Check:** The backend exchanges the Google ID token and performs a Firestore lookup on the `authorized_users` collection.
3.  **Scoped JWT Issuance:** If authorized and active, the backend signs a custom, stateless JSON Web Token (JWT) containing the user's allowed scopes, roles, and expiration.

## 🛠️ Admin CLI Tooling (`eldamo-admin`)

We provide a compiled command-line utility located in `./bin/eldamo-admin` (and run via `make`) to safely manage authorized users, roles, and scopes without exposing admin endpoints to the public internet.

### Environment Requirements
Ensure your terminal contains the appropriate Google Cloud credentials:
```bash
export FIREBASE_PROJECT_ID="testingproject-19c4c"
export FIREBASE_DATABASE="mithlond-services"
```

### 1. Authorizing New Users

Colleagues who want access can be authorized in two ways.

#### Method A: Pre-Registration (Recommended for onboarding)
You can pre-register a user before they even log in. Their record will stay inactive until they visit the Mithlond portal and authenticate.
```bash
./bin/eldamo-admin pre-register colleague@gmail.com
```
Once they log in at `https://www.mithlond.com/mcp-auth`, the server will automatically link their Firebase UID to this record and activate it.

#### Method B: Direct Authorization (By Email)
If they have already logged in at `https://www.mithlond.com/mcp-auth`, you can register them directly:
```bash
./bin/eldamo-admin add-email colleague@gmail.com
```

### 2. Inspecting User Directory
To list all currently registered users, their scopes, and active statuses:
```bash
./bin/eldamo-admin list
```

### 5. Managing Scopes
You can grant or revoke specific granular scopes to a user's authorized record in real-time:

```bash
# Grant an additional scope (e.g., audio:generate)
./bin/eldamo-admin grant <UID> audio:generate

# Revoke a scope
./bin/eldamo-admin revoke-scope <UID> audio:generate
```
*(Changes take effect upon the user's next token refresh or re-authentication.)*

### 4. Issuing Manual Handshake Tokens (Diagnostic)
To bypass the browser-based OAuth dance entirely and generate a 1-hour secure JWT access token for testing local or remote clients:
```bash
# Using the make target helper
make token UID=<USER-UID>

# Using the binary directly
./bin/eldamo-admin token <USER-UID>
```


## 🔒 Token Scopes & Permissions

Our MCP server checks for specific scopes inside the `MITHLOND_ACCESS_TOKEN` JWT claim when enforcing tool authorization:

| Scope Name | Bound Tool | Description |
| :--- | :--- | :--- |
| `lexicon:read` | `enquire_lexicon`<br/>`get_word_details`<br/>`get_derivations`<br/>`get_root_anchors` | General reading, spelling search, historical note retrieval, and semantic derivation tree queries. |
| `audio:generate` | `render_elvish_audio` | Access to the conditional Kokoro-based G2P/TTS pronunciation synthesis engine. |

### How Scopes are Enforced
*   **The Backend Gating:** Endpoints like `/sse` are wrapped with the `gate("lexicon:read", secureHandler)` middleware, which decodes the JWT claims and rejects requests lacking that exact scope with `403 Forbidden`.
*   **The Client Request:** During dynamic Handshakes, OpenCode parses the allowed scopes returned under the `Scope` field in `/api/oauth/token` (which we serialize dynamically as space-separated values, e.g. `"lexicon:read audio:generate"`).

---

## 🚨 Troubleshooting Reference

### "Redirect URI not authorized for client"
*   **The Cause:** The port or path requested by the client (e.g. `http://127.0.0.1:19876/mcp/oauth/callback`) does not match the strict whitelist in your deployed `https://www.mithlond.com/metadata.json` document.
*   **The Solution:** Confirm the port is dynamic loopback. Our server is RFC 8252 compliant and will match any loopback port automatically *if* the path matches. Ensure `"http://127.0.0.1:8080/mcp/oauth/callback"` is listed in the `redirect_uris` array on `mithlond.com/metadata.json` (making sure `/mcp/oauth/callback` matches the path).

### "User not authorized or inactive"
*   **The Cause:** The user has signed in on the portal, but their UID does not exist or is marked `active: false` in Firestore.
*   **The Solution:** Copy their UID from the Cloud Run logs and run `./bin/eldamo-admin add-email <email>` to lookup and add them with active status.
