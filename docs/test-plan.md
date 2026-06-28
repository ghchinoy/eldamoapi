# Test Plan: Eldamo Dual-Protocol Server

This plan covers the MCP (transactional) and A2A (interactional) surfaces, their
shared OAuth gate, and the cross-project conformance loop with
[`a2acli`](https://github.com/ghchinoy/a2acli). It documents both the **automated**
suite (`make test`) and the **manual / integration** procedures used to validate
the A2A wiring.

## 0. Prerequisites

```bash
brew install graphviz webp          # diagrams (optional)
go version                          # >= 1.25.5
golangci-lint --version             # project linter
```

Quality gates (must pass before merge/deploy):

```bash
make test                # unit + integration tests
golangci-lint run ./...  # must report "0 issues."
go build ./...           # must succeed
```

---

## 1. Automated tests (`make test`)

| Area | File | What it covers |
| :--- | :--- | :--- |
| Index build & search | `index/index_test.go` | Trie prefix, inverted keyword, derivations, root anchors |
| Tool handlers / search | `main_test.go`, `search_test.go` | MCP tool behavior, filters, dedup, caps |
| OAuth / CIMD / SSRF / middleware | (root package tests) | JWT verify, CIMD parsing, SSRF dialer, redirect matching |

Run: `make test` (verbose) or `go test ./...`.

### Gaps to close (tracked in `bd`)
- **A2A executor unit test** — assert `echoAgentExecutor.Execute` yields the
  expected message for given parts (Phase 1 follow-up).
- **AgentCard handler test** — `handleAgentCard` returns valid JSON, correct
  `scheme://host` derivation (honoring `X-Forwarded-Host`), and CORS headers.
- **A2A auth integration test** — `httptest` server with `oauthMiddleware`
  wrapping `/a2a`: assert 401 without token, 200 with a valid `make token` JWT.

---

## 2. Manual / integration: A2A surface

Start the server locally. Use a non-default port if `8080` is busy.

```bash
# Terminal A — server with auth ENFORCED
PORT=8099 make run            # or: PORT=8099 go run .

# Terminal A (alt) — auth BYPASSED for plumbing checks
AUTH_BYPASS=true PORT=8099 go run .
```

### 2.1 AgentCard discovery (public, no auth)

```bash
curl -s http://127.0.0.1:8099/.well-known/agent-card.json | jq .
```
**Expect:** HTTP 200; JSON with `name: "Eldamo Elvish Agent"`, a `supportedInterfaces`
entry whose `url` ends in `/a2a` and `protocolBinding: "JSONRPC"`, and an `echo`
skill. Confirm the `url` host matches the request host (Cloud Run / proxy correctness).

Via the CLI:
```bash
a2acli discover --service-url http://127.0.0.1:8099
```
**Expect:** Agent name, version, `Supported Bindings: JSONRPC`, `Streaming: true`,
and the `echo` skill listed.

### 2.2 Send a message (echo executor)

> Note: `a2acli`'s default streaming mode opens a Bubble Tea TUI and needs a TTY.
> In non-interactive shells / CI use `--wait` (blocking) or `--immediate`.

```bash
a2acli send "elen sila lumenn omentielvo" \
  --service-url http://127.0.0.1:8099 --transport jsonrpc --wait
```
**Expect (AUTH_BYPASS):** `Agent: Echo from Eldamo: elen sila lumenn omentielvo`

### 2.3 Auth enforcement (server WITHOUT `AUTH_BYPASS`)

```bash
# Card stays public
curl -s -o /dev/null -w "%{http_code}\n" \
  http://127.0.0.1:8099/.well-known/agent-card.json          # -> 200

# /a2a rejects missing token
curl -s -w "\nHTTP %{http_code}\n" -X POST http://127.0.0.1:8099/a2a \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"message/send","params":{}}'   # -> 401

# /a2a accepts a valid JWT — export JWT_SIGNING_KEY so make token's child
# process (go run ./cmd/eldamo-admin/) can inherit it.
# Plain 'source .env' only sets a shell variable; use set -a to auto-export.
set -a; source .env; set +a
TOKEN=$(make token)   # defaults UID=dev-user
a2acli send "Namarie" --service-url http://127.0.0.1:8099 \
  --transport jsonrpc --wait --token "$TOKEN"                 # -> echo succeeds
```

> **Export gotcha:** `.env` uses `VAR=value` (no `export`). Plain `source .env`
> sets a shell variable that child processes — including `go run` inside
> `make token` — cannot inherit. Use `set -a; source .env; set +a` to
> auto-export all variables, or `export JWT_SIGNING_KEY` after sourcing.
> Without this, `make token` falls back to `"temporary-dev-signing-key-mithlond"`
> and Cloud Run rejects the token with 401.

### 2.4 MCP regression (no breakage)

```bash
curl -s -o /dev/null -w "%{http_code}\n" -X POST http://127.0.0.1:8099/sse -d '{}'  # -> 401
```
MCP tools themselves are exercised by `make test` and by any MCP client (opencode /
Claude Desktop) per the README configuration.

---

## 3. Auth / token matrix

| Scenario | Token | Expected `/a2a` | Expected `/sse` |
| :--- | :--- | :--- | :--- |
| Local plumbing | `AUTH_BYPASS=true` | 200 | 200 |
| No token | — | 401 | 401 |
| Malformed / non-HMAC | garbage | 401 | 401 |
| Valid access JWT | `make token` | 200 | 200 |
| Wrong `type` claim | refresh token | 401 | 401 |
| Expired | aged JWT | 401 | 401 |

`make token UID=<uid>` mints a 1-hour HS256 access token signed with
`JWT_SIGNING_KEY` (falls back to the dev key). It must match the server's key.

---

## 4. Conformance loop with a2acli

`a2acli` is the reference real-world A2A client for this server. Validate against it
whenever the A2A surface changes:

1. `a2acli discover` — card parses, transports negotiate.
2. `a2acli send --wait` — blocking message round-trip.
3. `a2acli send --immediate` — fire-and-forget.
4. `a2acli get <taskID>` / `subscribe` — once tasks are real (post-Phase 3).
5. `a2acli --token` — auth passthrough against the enforced server.

Findings that require client-side work are filed as `bd` tasks in the `a2acli`
repo (see that project's `bd list`).

---

## 5. CI gate checklist

- [ ] `go build ./...`
- [ ] `make test`
- [ ] `golangci-lint run ./...` → `0 issues.`
- [ ] `a2acli discover` against a local instance succeeds
- [ ] `/a2a` returns 401 without a token, 200 with `make token`
- [ ] `/sse` still 401 without a token (MCP regression)
