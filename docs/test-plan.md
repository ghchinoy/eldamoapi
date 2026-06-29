# Test Plan: Eldamo Dual-Protocol Server

This plan covers the MCP (transactional) and A2A (interactional) surfaces, their
shared OAuth gate, and the cross-project conformance loop with
[`a2acli`](https://github.com/ghchinoy/a2acli). It documents both the **automated**
suite (`make test`) and the **manual / integration** procedures for each phase.

---

## 0. Prerequisites & quality gates

```bash
brew install graphviz webp          # diagrams (optional)
go version                          # >= 1.25.5
golangci-lint --version             # project linter
```

Quality gates — **must pass before merge/deploy:**

```bash
make test                # unit + integration tests
golangci-lint run ./...  # must report "0 issues."
go build ./...           # must succeed
```

**Token setup** — always needed for auth-enforced tests:

```bash
# .env uses bare VAR=value; set -a auto-exports so child processes inherit it
set -a; source .env; set +a
TOKEN=$(make token)          # default UID=dev-user; override: make token UID=alice
```

---

## 1. Automated tests (`make test`)

| Area | File(s) | What it covers |
| :--- | :--- | :--- |
| Index build & search | `index/index_test.go` | Trie prefix, inverted keyword, derivations, root anchors |
| MCP tool handlers | `main_test.go`, `search_test.go` | Tool behaviour, filters, dedup, caps |
| OAuth / CIMD / SSRF / middleware | `main_test.go` | JWT verify, CIMD parsing, SSRF dialer, redirect matching |
| A2A handler + AgentCard | `a2a_test.go` | Card HTTP surface, OAuth2 schemes, auth matrix (401/403/200), executor routing, scope rejection |
| Compounding & parsing | `a2a_test.go` | `joinRoots` (vowel elision, consonant assimilation), `parseNameRequest`, `isNameRequest`, `isUsableWord` |
| Translate skill | `a2a_test.go` | `isTranslateRequest`, `parseTranslateRequest`, `conceptsFromText`, `TranslateEnabled` |
| Taskstore — in-memory isolation | `taskstore_test.go` | Proves tasks disappear across separate `InMemory` instances (regression baseline) |
| Taskstore — Firestore roundtrip | `taskstore_test.go` | Cross-instance persistence, OCC stale-reject. **Skipped when `FIREBASE_PROJECT_ID` unset** |
| Taskstore — per-user isolation | `taskstore_test.go` | `List` returns only the requesting user's tasks. **Skipped when `FIREBASE_PROJECT_ID` unset** |

**Run without Firebase credentials (unit tests only):**
```bash
go test ./...
```

**Run with Firebase credentials (includes Firestore integration tests):**
```bash
set -a; source .env; set +a
go test ./...
# TestTaskstoreFirestore* tests will run and hit the real mithlond-services DB
```

### Known test gaps (tracked in `bd`)
- Neologism skill unit tests (`isNeologismRequest`, `parseNeologismRequest`, `splitNeologismPaths`)
- Full cursor-based pagination for `FirestoreTaskStore.List`

---

## 2. Manual / integration: A2A surface

### Server startup

```bash
# Auth BYPASSED — fastest for plumbing checks
AUTH_BYPASS=true PORT=8099 go run .

# Auth ENFORCED — use for all scope and token tests
set -a; source .env; set +a && PORT=8099 go run .
```

On startup, check the log confirms which task store is active:
```
[A2A] Using Firestore task store (a2a_tasks collection)   ← production mode
[A2A] Using in-memory task store (no Firestore client)    ← local / AUTH_BYPASS only
```

---

### 2.1 AgentCard discovery (public, no auth)

```bash
curl -s http://127.0.0.1:8099/.well-known/agent-card.json | jq '{version, skills: [.skills[].id], securitySchemes: (.securitySchemes | keys)}'
```

**Expect:**
```json
{
  "version": "0.4.0",
  "skills": ["name-generate", "neologism", "translate", "echo"],
  "securitySchemes": ["mithlond-oauth"]
}
```

Confirm `supportedInterfaces[0].url` uses the request host (not `localhost` when
behind Cloud Run):
```bash
curl -s https://candir.mithlond.com/.well-known/agent-card.json | jq '.supportedInterfaces[0].url'
# -> "https://candir.mithlond.com/a2a"
```

Via `a2acli`:
```bash
a2acli discover --service-url http://127.0.0.1:8099
# Expect: version 0.4.0, all four skills, Security: mithlond-oauth
```

---

### 2.2 Auth enforcement

```bash
# Card is always public
curl -s -o /dev/null -w "%{http_code}\n" \
  http://127.0.0.1:8099/.well-known/agent-card.json     # -> 200

# /a2a rejects missing token
curl -s -o /dev/null -w "%{http_code}\n" -X POST \
  http://127.0.0.1:8099/a2a -d '{}'                     # -> 401

# /a2a accepts a valid token
set -a; source .env; set +a && TOKEN=$(make token)
a2acli send "Aiya" --service-url http://127.0.0.1:8099 \
  --transport jsonrpc --wait --token "$TOKEN"
# -> Agent: Echo from Eldamo: Aiya
```

> **Export gotcha:** `.env` uses `VAR=value` (no `export`). Plain `source .env`
> only sets shell variables; child processes (including `go run` inside `make token`)
> cannot inherit them. Always use `set -a; source .env; set +a`.

---

### 2.3 Scope gating (Phase 2)

```bash
set -a; source .env; set +a && TOKEN=$(make token)

# MCP /sse — requires lexicon:read
curl --max-time 2 -s -o /dev/null -w "HTTP %{http_code}\n" \
  -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8099/sse
# -> HTTP 200  (SSE stream opens; Ctrl-C)

# A2A /a2a without token -> 401
curl -s -o /dev/null -w "%{http_code}\n" -X POST http://127.0.0.1:8099/a2a -d '{}'
# -> 401

# Token missing agent:invoke -> interceptor rejection (HTTP 200, JSON-RPC error body)
# Token missing skill:name-generate -> "Insufficient scope" message from executor
```

---

### 2.4 Skills (Phases 3–3c)

#### Setup
```bash
AUTH_BYPASS=true PORT=8099 go run .
# Or with token: set -a; source .env; set +a && PORT=8099 go run .
TOKEN=$(make token)   # not needed for AUTH_BYPASS
```

#### Echo (diagnostic fallback)
```bash
a2acli send "Namarie" --service-url http://127.0.0.1:8099 \
  --transport jsonrpc --wait [--token "$TOKEN"]
# -> Agent: Echo from Eldamo: Namarie
```

#### Name-generate (deterministic, Phase 3)

Input format: `name <concept1> [concept2] [quenya|sindarin] [masculine|feminine]`

```bash
a2acli send "name star silver quenya" \
  --service-url http://127.0.0.1:8099 --transport jsonrpc --wait [--token "$TOKEN"]
# -> 1 ARTIFACT: **Elenyasilmandil** — roots: elenya + silma, suffix -ndil

a2acli send "name grey flame sindarin" \
  --service-url http://127.0.0.1:8099 --transport jsonrpc --wait [--token "$TOKEN"]
# -> 1 ARTIFACT: Sindarin compound with -on suffix
```

#### Translate (LLM-backed, Phase 3b)

*Requires `GEMINI_TRANSLATE_MODEL` set in env.*

```bash
set -a; source .env; set +a   # ensures GEMINI_TRANSLATE_MODEL reaches server
PORT=8099 go run . &           # restart with model env var

a2acli send "translate farewell my friend to quenya" \
  --service-url http://127.0.0.1:8099 --transport jsonrpc --wait --token "$TOKEN"
# -> 1 ARTIFACT: Gemini analysis + Quenya translation (namárië path)

a2acli send "translate to sindarin: the grey havens" \
  --service-url http://127.0.0.1:8099 --transport jsonrpc --wait --token "$TOKEN"
# -> 1 ARTIFACT: hithren + lond/lynd with Sindarin mutation analysis
```

#### Neologism (LLM-backed, two artifacts, Phase 3c)

```bash
a2acli send "neologism hover-board quenya" \
  --service-url http://127.0.0.1:8099 --transport jsonrpc --wait --token "$TOKEN"
# -> 2 ARTIFACTS: "Practical Path" and "Poetic Path"

a2acli send "coin a word for artificial intelligence sindarin" \
  --service-url http://127.0.0.1:8099 --transport jsonrpc --wait --token "$TOKEN"
# -> 2 ARTIFACTS: both Sindarin paths with 100-point scoring
```

---

### 2.5 Firestore taskstore (Phase 5)

The Firestore task store enables `get`, `subscribe`, and `list tasks` to work
across Cloud Run instances and restarts.

> **Which skills create tasks?** Only `name-generate`, `translate`, and `neologism`
> go through the task state machine (`NewSubmittedTask` → Firestore write → Task ID
> returned). The `echo` fallback yields a bare `*Message` and creates no task — it
> will show `Task ID: ` empty and nothing is written to `a2a_tasks`. Always use a
> skill, not echo, when testing the taskstore.

**Verify the store is active** (check server startup log):
```bash
grep "Firestore task store\|in-memory task store" <server log>
# Production: "[A2A] Using Firestore task store (a2a_tasks collection)"
```

#### Task ID capture and retrieval

```bash
set -a; source .env; set +a && TOKEN=$(make token)

# 1. Send a message and note the Task ID from the output
a2acli send "name star quenya" \
  --service-url https://candir.mithlond.com \
  --transport jsonrpc --wait --token "$TOKEN"
# -> "Task ID: 019f1234-abcd-..."   ← copy this

TASK_ID="019f1234-abcd-..."   # paste here

# 2. Retrieve the task by ID (may hit a different Cloud Run instance)
a2acli get "$TASK_ID" \
  --service-url https://candir.mithlond.com \
  --transport jsonrpc --token "$TOKEN"
# -> Task Status: [TASK_STATE_COMPLETED], 1 ARTIFACT(S) AVAILABLE
# Failure mode (in-memory only): "task not found" 
```

#### List your tasks (per-user isolation)

```bash
a2acli list tasks \
  --service-url https://candir.mithlond.com \
  --transport jsonrpc --token "$TOKEN"
# -> Shows only tasks created with this token's sub (dev-user)
# Other users' tasks must not appear
```

#### Cross-instance persistence verification

The simplest check is: send on one request, wait, get on a subsequent request.
Cloud Run may route to a different instance each time. If the task is found,
the Firestore store is working.

```bash
# Send (instance A)
a2acli send "name ocean sindarin" \
  --service-url https://candir.mithlond.com \
  --transport jsonrpc --wait --token "$TOKEN"
# Note the Task ID

# Get 30+ seconds later (likely a different instance)
sleep 30
a2acli get "$TASK_ID" \
  --service-url https://candir.mithlond.com \
  --transport jsonrpc --token "$TOKEN"
# -> Task found with COMPLETED status = Firestore persistence confirmed
```

#### Automated taskstore tests (with Firebase credentials)

```bash
set -a; source .env; set +a
go test -v -run "TestTaskstore" .
```

Expected output:
```
--- PASS: TestTaskstoreInMemoryIsolation      (0.00s)
    ✓ In-memory store correctly isolates tasks across instances (expected limitation)
--- PASS: TestTaskstoreFirestoreRoundtrip     (1.8s)
    ✓ Firestore store persists tasks across instances
    ✓ Optimistic concurrency correctly rejects stale updates
--- PASS: TestTaskstoreFirestorePerUserIsolation (0.9s)
    ✓ Firestore List correctly isolates tasks per user
--- PASS: TestTaskstoreFirestoreNotFound      (0.2s)
```

---

### 2.6 MCP regression (no breakage)

```bash
curl -s -o /dev/null -w "%{http_code}\n" -X POST http://127.0.0.1:8099/sse -d '{}'
# -> 401  (MCP gate still active)
```

MCP tools are also exercised by `make test` and by any MCP client (opencode /
Claude Desktop) per the README configuration.

---

## 3. Auth / token matrix

| Scenario | Token | `/a2a` | `/sse` |
| :--- | :--- | :--- | :--- |
| Local plumbing | `AUTH_BYPASS=true` | 200 | 200 |
| No token | — | 401 | 401 |
| Malformed / non-HMAC | garbage | 401 | 401 |
| Valid access JWT (`make token`) | ✓ | 200 | 200 |
| Wrong `type` claim (refresh token) | — | 401 | 401 |
| Expired | — | 401 | 401 |
| Valid JWT, wrong signing key | — | 401 | 401 |

---

## 4. Scope matrix

| Token scopes | `/sse` | echo | name-generate | translate | neologism |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `AUTH_BYPASS` | 200 | ✓ | ✓ | ✓ | ✓ |
| All scopes (`make token`) | 200 | ✓ | ✓ | ✓ | ✓ |
| `lexicon:read` only | 200 | ✗ interceptor | ✗ | ✗ | ✗ |
| `agent:invoke` only | 403 | ✓ | ✗ scope msg | ✗ | ✗ |
| `agent:invoke` + `skill:name-generate` | 403 | ✓ | ✓ | ✗ | ✗ |
| none / no token | 401 | 401 | 401 | 401 | 401 |

---

## 5. Conformance loop with a2acli

`a2acli` is the reference real-world A2A client. Validate against it whenever
the A2A surface or taskstore changes.

### Quick smoke check (one command)

```bash
set -a; source .env; set +a
a2acli conformance \
  --service-url https://candir.mithlond.com \
  --token "$(make token)"
# Runs: AgentCard validation → auth gating → round-trip message
# All steps must show PASS
```

### Full conformance sequence

```bash
set -a; source .env; set +a && TOKEN=$(make token)
SVC=https://candir.mithlond.com

# 1. Card parses + skills listed
a2acli discover --service-url $SVC

# 2. Blocking round-trip
a2acli send "Namarie" --service-url $SVC \
  --transport jsonrpc --wait --token "$TOKEN"

# 3. Fire-and-forget (immediate return, no wait for completion)
a2acli send "name star quenya" --service-url $SVC \
  --transport jsonrpc --immediate --token "$TOKEN"

# 4. Retrieve by task ID (tests Firestore persistence)
a2acli get <taskID-from-step-3> --service-url $SVC --token "$TOKEN"

# 5. List tasks (tests per-user isolation)
a2acli list tasks --service-url $SVC --token "$TOKEN"

# 6. Auth passthrough — correct token accepted, missing token rejected
curl -s -o /dev/null -w "%{http_code}\n" \
  -X POST $SVC/a2a -d '{}'        # -> 401
```

---

## 6. CI gate checklist

- [ ] `go build ./...`
- [ ] `make test` (unit tests, no Firebase creds required)
- [ ] `set -a; source .env; set +a && go test ./...` (includes Firestore taskstore tests)
- [ ] `golangci-lint run ./...` → `0 issues.`
- [ ] `a2acli conformance --service-url https://candir.mithlond.com --token "$(make token)"` → all PASS
- [ ] `a2acli get <taskID>` after `send --immediate` succeeds (Firestore persistence)
- [ ] `a2acli list tasks` returns only the requesting user's tasks
- [ ] `/sse` returns 401 without a token (MCP regression)
- [ ] AgentCard `supportedInterfaces[0].url` contains `candir.mithlond.com` (not `localhost`)
