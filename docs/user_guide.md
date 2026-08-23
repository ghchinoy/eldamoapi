# 🏹 Connecting to the Mithlond Eldamo MCP Server

Welcome! The Mithlond Eldamo MCP Server is a secure, high-performance Tolkien language lexicon service. You can connect your local AI coding assistant (such as Cursor, Claude Desktop, or opencode) directly to it to query Quenya, Sindarin, and Adûnaic entries in real-time.

Here is how to set up your connection in under 2 minutes.

---

## 1. Obtain Your Access Token

Because the server is secured using modern OAuth 2.1 protocol, any request without a valid bearer token is immediately blocked with `401 Unauthorized`. 

To connect, you need a personal **Access Token**:
* **From the Admin:** The server administrator can generate a secure Access Token for you.
* **From the Portal (Coming Soon):** Once the user portal is live at `www.mithlond.com`, you will be able to log in with your Google account and self-issue personal access tokens.

Once you have your token string, save it as an environment variable in your terminal/profile:
```bash
export MITHLOND_ACCESS_TOKEN="your-personal-jwt-token"
```


## 2. Configure Your AI Client

Choose your preferred coding assistant below and configure the remote MCP server block:

### 📂 Cursor (IDE)
1. Open Cursor and navigate to **Settings** -> **Models** -> **MCP**.
2. Click **+ Add New MCP Server**.
3. Configure the fields:
   * **Name:** `eldamo-remote`
   * **Type:** `SSE`
   * **URL:** `https://candir.mithlond.com/sse`
4. Under **Headers**, click **+ Add Header**:
   * **Key:** `Authorization`
   * **Value:** `Bearer your-personal-jwt-token` *(replace with your actual raw JWT)*
5. Click **Save**.

### 📂 Claude Desktop
Open your Claude Desktop configuration file:
* **macOS:** `~/Library/Application Support/Claude/claude_desktop_config.json`
* **Windows:** `%APPDATA%\Claude\claude_desktop_config.json`

Append the following `eldamo-remote` block inside your `mcpServers` object:
```json
{
  "mcpServers": {
    "eldamo-remote": {
      "type": "remote",
      "url": "https://candir.mithlond.com/sse",
      "headers": {
        "Authorization": "Bearer your-personal-jwt-token"
      }
    }
  }
}
```

### ✨ Gemini Spark (gemini.google.com Connected Apps)

Gemini Spark supports **automatic registration** — no token or pre-registration required.

**Requirements:** Gemini Spark access (US, personal Google Account, 18+, Keep Activity on).

1. Go to [gemini.google.com](https://gemini.google.com) → **Settings & help** → **Connected Apps**.
2. Under "Custom apps for Spark", enter the MCP server URL:
   ```
   https://candir.mithlond.com
   ```
   (The bare domain works; `/sse` also works.)
3. Click **Next**. Spark will:
   - Discover the server via `/.well-known/oauth-protected-resource` (RFC 9728)
   - Auto-register via `POST /api/oauth/register` (RFC 7591 DCR — "automatic registration")
   - Redirect to the Mithlond consent page at `https://www.mithlond.com/mcp-auth`
4. Sign in with your Google account and click **Approve and Connect**.
5. Spark redirects back and the connection is active.

> **Prerequisite:** Your Google UID must be added to the `authorized_users` Firestore collection by the server administrator before the consent step will succeed. Contact the admin with your Google account email.

> **Troubleshooting — "does not support automatic registration":** This error appears on older server deployments that predate RFC 9728 / DCR support. Ensure you are connecting to the current deployment (check `candir.mithlond.com` is the latest Cloud Run revision). If the error persists, use the manual client ID / secret fallback (advanced settings in the Spark UI), which is not currently supported — escalate to the admin.

---

### 📂 opencode / CLI Agents

You can configure opencode to use either **Pre-Authenticated** headers or the **Dynamic OAuth 2.1** flow:

#### Option A: Pre-Authenticated Header
Using your manually generated token from the administrator:
```json
{
  "mcp": {
    "eldamo-remote": {
      "type": "remote",
      "url": "https://candir.mithlond.com/sse",
      "headers": {
        "Authorization": "Bearer {env:MITHLOND_ACCESS_TOKEN}"
      },
      "enabled": true
    }
  }
}
```

#### Option B: Dynamic OAuth 2.1 (Seamless)
Let OpenCode handle the browser login flow and token lifecycle automatically:
```json
{
  "mcp": {
    "eldamo-remote": {
      "type": "remote",
      "url": "https://candir.mithlond.com/sse",
      "enabled": true,
      "oauth": {
        "clientId": "https://www.mithlond.com/metadata.json",
        "authorizationUrl": "https://www.mithlond.com/mcp-auth",
        "tokenUrl": "https://candir.mithlond.com/api/oauth/token"
      }
    }
  }
}
```


## 3. Troubleshooting Guide

If your assistant is having issues establishing a connection, check the common troubleshooting steps below:

### Issue A: `403 (Forbidden)` during authorize-callback (Google Login)
* **Description:** You click "Approve and Connect" in your browser but receive a `403 Forbidden` error.
* **The Cause:** 
  1. Your Firebase User UID is not authorized in Firestore.
  2. The dynamic client's redirect loopback URL (e.g. `http://127.0.0.1:19876/mcp/oauth/callback`) does not match the allowed patterns in the live `https://www.mithlond.com/metadata.json` document.
* **The Fix:**
  * Contact your administrator to add your Google Auth `UID` (found in Firestore console) to the `authorized_users` collection and mark it `active: true`.
  * Ensure the allowed `redirect_uris` in your web portal's `metadata.json` includes the correct host and path: `"http://127.0.0.1:8080/mcp/oauth/callback"`. Our server is RFC 8252 compliant and will match any dynamic port (like `19876`) automatically if the path and host match!

### Issue B: `401 Unauthorized: Invalid token type or claims`
* **Description:** The client is authenticated but requests are rejected with a 401 error.
* **The Cause:** The generated token is missing the required claim `"type": "access"`.
* **The Fix:** Ensure you generate tokens using the modern `eldamo-admin` tool instead of legacy/manual JWT scripts:
  ```bash
  # Generate a valid, typed token
  make token UID=your-user-uid
  ```

### Issue C: `Incompatible auth server: does not support dynamic client registration`
* **Description:** OpenCode halts diagnostic connections with this dynamic client registration error.
* **The Cause:** OpenCode uses CIMD (not DCR) and expects the OAuth discovery document to match what is in the local `opencode.json` `oauth` block. This error fires when OpenCode loaded a conflicting or stale configuration that omitted the `oauth` block, or is pointing at the raw Cloud Run URL instead of the `candir.mithlond.com` domain.
* **The Fix:** 
  * Ensure your `opencode.json` has the `oauth` block (see Option B above) with `clientId: "https://www.mithlond.com/metadata.json"`.
  * Ensure you don't have a redundant parent `opencode.json` overriding your local configuration with stale headers.
  * Run `opencode mcp auth logout eldamo-remote` and clear the local cache: `rm -rf ~/.cache/opencode/*` before authenticating again.

### Issue D: Gemini Spark consent page shows "Invalid Client ID URL"
* **Description:** The Mithlond consent SPA shows a confusing error label next to the client identity.
* **The Cause:** This was a display bug in the SPA (`mcp-auth.html`) that showed "Invalid Client ID URL" for DCR-issued (opaque) `client_id`s. It was fixed in `mithlond-web` commit `890b237`.
* **The Fix:** Ensure the `mithlond-web` Firebase Hosting deployment is up to date (`firebase deploy --only hosting` from the `mithlond-web` repo). The underlying authorization was always functional — only the display label was wrong.


## 4. Verify Your Connection

Restart your IDE or active CLI agent session for configuration changes to take effect. 

To test that your assistant is successfully communicating with the remote Tolkien lexicon database, ask it a linguistic query:

* *"Can you search the remote Eldamo lexicon for 'star'?"*
* *"What does the Elvish word 'elen' mean?"*

Your request will hit Cloud Run, go through our `oauthMiddleware` to verify your Bearer signature, and stream your results instantly! 🏹✨


## 5. A2A Agent Surface & Linguistic Skills

Alongside transactional MCP tools, the server exposes an **A2A (Agent2Agent)** endpoint for higher-level linguistic capabilities. While MCP tools provide raw lexicon lookups, A2A skills orchestrate end-to-end tasks like authentic name compounding, poetic translation, and phonologically rigorous neologisms.

* **Endpoint:** `https://candir.mithlond.com/a2a`
* **Agent Card (Discovery):** `https://candir.mithlond.com/.well-known/agent-card.json` (public, unauthenticated)
* **Authentication:** Uses the **same** Bearer JWT token as the MCP surface.

### 🛠️ Using the A2A Conformance CLI (`a2acli`)

Install and use `a2acli` to interact directly with the A2A agent:

```bash
# 1. Generate an access token (or retrieve from your admin)
set -a; source .env; set +a
TOKEN=$(make token UID=your-user-id)

# 2. Discover the agent card (pass base domain, NOT /a2a)
a2acli discover -u https://candir.mithlond.com

# 3. Send a message to the agent (always use --wait or --immediate in CLI/CI)
a2acli send "Generate a name for a star in Quenya" \
  -u https://candir.mithlond.com \
  --token "$TOKEN" \
  --wait --output text
```

> **Note on discovery:** `a2acli` appends `/.well-known/agent-card.json` automatically. Pass the base host URL (`-u https://candir.mithlond.com`), not the `/a2a` endpoint path.

---

### 🧠 Available Skills & Examples

The A2A agent automatically routes natural language queries to specialized skills, or you can invoke a specific skill directly using `--skill <id>`.

#### 1. `name-generate` (Elvish Name Generator)
Generates authentic Quenya or Sindarin personal, place, or weapon names by compounding historical roots and applying strict phonotactic sound laws:
* **Example Prompt:** `"name star silver quenya"`
* **Example Prompt:** `"name grey flame sindarin"`
* **Direct Skill Invocation:**
  ```bash
  a2acli send "silver star" -u https://candir.mithlond.com --token "$TOKEN" --skill name-generate --wait --output text
  ```
* **Required Scope:** `skill:name-generate`

#### 2. `translate` (Elvish Translator)
Translates English text into Quenya or Sindarin using grounded lexicon retrieval and Vertex AI Gemini:
* **Example Prompt:** `"translate farewell my friend to quenya"`
* **Example Prompt:** `"translate to sindarin: the grey havens"`
* **Direct Skill Invocation:**
  ```bash
  a2acli send "friend of stars" -u https://candir.mithlond.com --token "$TOKEN" --skill translate --wait --output text
  ```
* **Required Scope:** `skill:translate`

#### 3. `neologism` (Elvish Neologism Builder)
Constructs new Elvish vocabulary for modern concepts following historical Sound Laws:
* **Example Prompt:** `"neologism hover-board quenya"`
* **Example Prompt:** `"coin a word for artificial intelligence sindarin"`
* **Direct Skill Invocation:**
  ```bash
  a2acli send "chaos" -u https://candir.mithlond.com --token "$TOKEN" --skill neologism --wait --output text
  ```
* **Required Scope:** `skill:neologism`

#### 4. `echo` (Diagnostic Echo)
Echoes the input back to verify transport and authentication without calling linguistic backends:
* **Example Prompt:** `"hello"` or `"Namarie"`
* **Required Scope:** `agent:invoke`

---

### 🎙️ Audio Pronunciation (`render_elvish_audio`)

When running inside an MCP-enabled environment (or when TTS is configured), you can synthesize spoken Elvish audio using our Kokoro-based TTS proxy:

* **Tool Name:** `render_elvish_audio`
* **Parameters:**
  * `text` (required): The Elvish word or phrase to pronounce (e.g., `"Elen síla lúmenn’ omentielvo"`).
  * `voice` (optional): TTS voice identifier — `"sarah"` (default), `"bella"`, `"adam"`, `"emma"`.
  * `speed` (optional): Playback speed — defaults to `0.8` (recommended for clear Elvish phonemes).
* **Output:** Returns computed IPA phonemes and a streaming WAV audio URL (e.g. `https://lhongant.mithlond.com/api/audio/<hash>.wav`).

---

### 🔍 Client Transport Nuances & Tips

* **Stateless Streamable HTTP:** The server operates in stateless mode on `/sse` and `/`. If you restart your MCP client (such as Antigravity or opencode), your client will reconnect seamlessly without `404 session not found` errors.
* **Antigravity Desktop:** Antigravity Desktop sends an `X-Mcp-Force-Sse` header for long-lived streams. The server multiplexer routes this to the SSE handler while serving tool calls statelessly.
* **Standalone SSE Stream Notice:** Some clients (like Antigravity CLI) may briefly log a cosmetic notice regarding standalone SSE streams. Tool calls (including audio rendering) are unaffected and complete over Streamable HTTP.

