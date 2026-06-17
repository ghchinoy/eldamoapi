# 🏹 Connecting to the Mithlond Eldamo MCP Server

Welcome! The Mithlond Eldamo MCP Server is a secure, high-performance Tolkien language lexicon service. You can connect your local AI coding assistant (such as Cursor, Claude Desktop, Windsurf, or opencode) directly to it to query Quenya, Sindarin, and Adûnaic entries in real-time.

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

---

## 2. Configure Your AI Client

Choose your preferred coding assistant below and configure the remote MCP server block:

### 📂 Cursor (IDE)
1. Open Cursor and navigate to **Settings** -> **Models** -> **MCP**.
2. Click **+ Add New MCP Server**.
3. Configure the fields:
   * **Name:** `eldamo-remote`
   * **Type:** `SSE`
   * **URL:** `https://www.mithlond.com/sse`
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
      "url": "https://www.mithlond.com/sse",
      "headers": {
        "Authorization": "Bearer your-personal-jwt-token"
      }
    }
  }
}
```

### 📂 opencode / CLI Agents
Add the following to your local workspace `opencode.json` or global `~/.config/opencode/opencode.json`:
```json
{
  "mcp": {
    "eldamo-remote": {
      "type": "remote",
      "url": "https://www.mithlond.com/sse",
      "headers": {
        "Authorization": "Bearer {env:MITHLOND_ACCESS_TOKEN}"
      },
      "enabled": true
    }
  }
}
```

---

## 3. Verify Your Connection

Restart your IDE or active CLI agent session for configuration changes to take effect. 

To test that your assistant is successfully communicating with the remote Tolkien lexicon database, ask it a linguistic query:

* *"Can you search the remote Eldamo lexicon for 'star'?"*
* *"What does the Elvish word 'elen' mean?"*

Your request will hit Cloud Run, go through our `oauthMiddleware` to verify your Bearer signature, and stream your results instantly! 🏹✨
