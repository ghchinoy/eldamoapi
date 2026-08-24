# 🛡️ Mithlond Eldamo MCP Server: Administrator's Guide

This document is the official operational guide for system administrators managing the **Mithlond Eldamo MCP & A2A Server** authentication gateway, user scopes, access requests, and diagnostic flows.

---

## 🏗️ Identity & Authorization Architecture

Our security pipeline separates **Identity** (who the user is, verified securely by Google/Firebase Auth) from **Authorization** (what resources/tools the user can access, managed dynamically in our Firestore database).

1. **Identity Verification:** The desktop client (Cursor, Claude, OpenCode, Spark, Antigravity) or web frontend authenticates via Google Auth on `https://www.mithlond.com/mcp-auth`.
2. **Authorization Check:** The backend exchanges the Google ID token and performs a Firestore lookup on the `authorized_users` collection in the `mithlond-services` database.
3. **Scoped JWT Issuance:** If authorized and active, the backend signs a custom, stateless JSON Web Token (JWT) containing the user's allowed scopes, roles, and expiration.

---

## 🛠️ Admin CLI Tooling (`eldamo-admin`)

We provide a compiled command-line utility located in `./bin/eldamo-admin` (built from `cmd/eldamo-admin/main.go`, styled with **Charm Lip Gloss**) to safely manage authorized users, incoming access requests, roles, and scopes without exposing admin endpoints to the public internet.

### Environment Requirements
The tool defaults to `FIREBASE_PROJECT_ID=testingproject-19c4c` and `FIREBASE_DATABASE=mithlond-services`. To override:
```bash
export FIREBASE_PROJECT_ID="testingproject-19c4c"
export FIREBASE_DATABASE="mithlond-services"
```

### Build & Run
```bash
# Build the binary
go build -o bin/eldamo-admin ./cmd/eldamo-admin

# View available commands
./bin/eldamo-admin --help
```

---

## 📋 Common Administrative Tasks

### 1. Reviewing & Approving Access Requests
Users can submit access requests directly via the web portal at `https://www.mithlond.com/mcp-auth`.

```bash
# List all incoming workspace access requests
./bin/eldamo-admin requests list

# Approve an applicant by their Google UID
./bin/eldamo-admin requests approve <FIREBASE_UID>
```
*Approving automatically adds the user to `authorized_users` with `active: true` and the **minimal baseline scopes** (`lexicon:read`, `agent:invoke`, `skill:name-generate`), and marks the request document as `status: approved`.*

### 2. Inspecting the User Directory
To view all currently registered users, their scopes, roles, and active statuses in a formatted table:
```bash
./bin/eldamo-admin list
```

### 3. Adding Users Manually

#### Method A: Pre-Registration (By Email)
Pre-register a colleague before they log in. Their record will stay `inactive` with minimal baseline scopes until they visit the Mithlond portal and authenticate with their Google account:
```bash
./bin/eldamo-admin pre-register colleague@example.com
```

#### Method B: Direct Authorization (By UID)
Add a user directly by their verified Firebase UID (grants minimal baseline scopes):
```bash
./bin/eldamo-admin add <FIREBASE_UID> colleague@example.com
```

### 4. Managing & Upgrading Scopes
Newly approved users receive the minimal baseline tier. Grant cost-bearing or advanced scopes (TTS audio, LLM translation/neologisms) individually as needed:
```bash
# Upgrade: Grant audio TTS capability to a user (by UID or email)
./bin/eldamo-admin grant <UID-or-EMAIL> audio:generate

# Upgrade: Grant LLM-backed skills to a user
./bin/eldamo-admin grant <UID-or-EMAIL> skill:translate
./bin/eldamo-admin grant <UID-or-EMAIL> skill:neologism

# Revoke a scope
./bin/eldamo-admin revoke-scope <UID-or-EMAIL> audio:generate

# Grant new scopes to ALL authorized users (idempotent, safe migration helper)
./bin/eldamo-admin grant-all skill:name-generate skill:translate skill:neologism
```
*(Changes take effect upon the user's next token refresh or re-authentication.)*

### 5. Generating Handshake Access Tokens (Diagnostics & CI)
To bypass the browser-based OAuth flow and generate a 1-hour secure JWT access token with full scopes for testing local or remote clients:
```bash
# Using the make target helper
make token UID=<USER-UID>

# Using the binary directly
./bin/eldamo-admin token <USER-UID>
```

---

## 🔒 Token Scopes & Permissions Matrix

Our server verifies specific scopes inside the signed `MITHLOND_ACCESS_TOKEN` JWT claims. Scopes are organized into two tiers: **Baseline** (granted automatically upon onboarding) and **Upgrade-Only** (granted individually to manage cost and rate limits).

| Scope Name | Tier | Bound Tools / Endpoints | Description |
| :--- | :--- | :--- | :--- |
| `lexicon:read` | **Baseline** | `enquire_lexicon`<br/>`get_word_details`<br/>`get_derivations`<br/>`get_root_anchors` | General reading, spelling search, historical note retrieval, and semantic derivation tree queries. (Free, in-memory) |
| `agent:invoke` | **Baseline** | `POST /a2a` | Send A2A task invocation messages to the Eldamo interactional agent. |
| `skill:name-generate` | **Baseline** | `name-generate` skill | Execute deterministic Elvish personal, place, and weapon compounding. (Free, deterministic) |
| `skill:translate` | *Upgrade* | `translate` skill | Execute LLM-grounded translation into Quenya or Sindarin. (External LLM API) |
| `skill:neologism` | *Upgrade* | `neologism` skill | Execute two-path neologism creation with 100-point phonotactic scoring. (External LLM API) |
| `audio:generate` | *Upgrade* | `render_elvish_audio` | Access to the Kokoro-based G2P/TTS neural pronunciation synthesis engine. (External TTS proxy) |

---

## 🚨 Troubleshooting Reference

### "Redirect URI not authorized for client"
* **The Cause:** The port or path requested by the client does not match the allowed redirect URIs.
* **The Solution:** Confirm the client uses dynamic loopback (RFC 8252). Our server matches loopback ports automatically as long as the path matches `/mcp/oauth/callback` or `/oauth/callback`.

### "User not authorized or inactive"
* **The Cause:** The user has signed in on the portal, but their UID does not exist or is marked `active: false` in the `authorized_users` collection.
* **The Solution:** Run `./bin/eldamo-admin requests list` to find their pending UID and run `./bin/eldamo-admin requests approve <UID>`.
