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
   * **URL:** `https://eldamo-mcp-server-308690897031.us-central1.run.app/sse`
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
      "url": "https://eldamo-mcp-server-308690897031.us-central1.run.app/sse",
      "headers": {
        "Authorization": "Bearer your-personal-jwt-token"
      }
    }
  }
}
```

### 📂 opencode / CLI Agents

You can configure opencode to use either **Pre-Authenticated** headers or the **Dynamic OAuth 2.1** flow:

#### Option A: Pre-Authenticated Header
Using your manually generated token from the administrator:
```json
{
  "mcp": {
    "eldamo-remote": {
      "type": "remote",
      "url": "https://eldamo-mcp-server-308690897031.us-central1.run.app/sse",
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
      "url": "https://eldamo-mcp-server-308690897031.us-central1.run.app/sse",
      "enabled": true,
      "oauth": {
        "clientId": "https://www.mithlond.com/metadata.json",
        "authorizationUrl": "https://www.mithlond.com/mcp-auth",
        "tokenUrl": "https://eldamo-mcp-server-308690897031.us-central1.run.app/api/oauth/token"
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
* **The Cause:** OpenCode failed to read the OAuth capability hints from the server or loaded a conflicting local `opencode.json` configuration that omitted the `oauth` block.
* **The Fix:** 
  * Ensure you don't have a redundant parent `opencode.json` overriding your local configuration with stale headers (like static environment variables).
  * Run `opencode mcp auth logout eldamo-remote` and clear the local cache: `rm -rf ~/.cache/opencode/*` before authenticating again.


## 4. Verify Your Connection

Restart your IDE or active CLI agent session for configuration changes to take effect. 

To test that your assistant is successfully communicating with the remote Tolkien lexicon database, ask it a linguistic query:

* *"Can you search the remote Eldamo lexicon for 'star'?"*
* *"What does the Elvish word 'elen' mean?"*

Your request will hit Cloud Run, go through our `oauthMiddleware` to verify your Bearer signature, and stream your results instantly! 🏹✨
