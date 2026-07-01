# Running Local LLM Servers (llama.cpp / mlx_lm.server)

This is the reference guide for standing up a local, OpenAI-compatible LLM
server to power the `translate` and `neologism` A2A skills instead of Vertex
AI Gemini — useful for testing local or fine-tuned Gemma 4 models (GGUF and
MLX) without any cloud dependency or cost. See epic `eldamo-server-hk1`
(`bd show eldamo-server-hk1`) for the roadmap tracking this work, and its
child issue `eldamo-server-gut` for implementation notes.

For the eldamo-server-side environment variables (`LOCAL_LLM_BASE_URL`,
`LOCAL_LLM_MODEL`, `LOCAL_LLM_RUNTIME`, `LOCAL_LLM_MAX_TOKENS`), see
[DEVELOPMENT.md](DEVELOPMENT.md#4-using-a-local-llm-backend-for-translateneologism-llamacpp-or-mlx_lmserver).
This doc is the deeper reference for the two server runtimes themselves.

For functional/regression testing of the eldamo-server integration, see
[test-plan.md §3](test-plan.md#3-local-llm-backend-testing-gemma-4-via-llamacpp--mlx_lmserver).
For comparing response quality and cost across backends/models, see
[model-evaluation.md](model-evaluation.md).

---

## Why two runtimes?

Both [llama.cpp](https://github.com/ggml-org/llama.cpp)'s `llama-server` and
[mlx-lm](https://github.com/ml-explore/mlx-lm)'s `mlx_lm.server` expose an
**identical** OpenAI-compatible `/v1/chat/completions` streaming API
(confirmed by inspecting `mlx_lm/server.py`, which routes the same paths and
returns the same `usage{prompt_tokens,completion_tokens}` shape as
llama.cpp). eldamo-server's `localLLMClient` (`llm_local.go`) therefore
speaks to both through one code path — the only difference is which server
you point `LOCAL_LLM_BASE_URL` at.

| | llama.cpp `llama-server` | mlx-lm `mlx_lm.server` |
|---|---|---|
| Model format | GGUF (quantized) | MLX (native Apple Silicon format) |
| Acceleration | CPU / Metal (via GGML) | Metal (native MLX) |
| Best for | Portability, widest quantization support | Apple Silicon-native throughput |
| One process = | One model | One model (see [gotchas](#gotcha-1-the-model-field-is-not-cosmetic-in-mlx_lmserver)) |

---

## Prerequisites

```bash
# llama.cpp (provides llama-server, llama-cli, llama-quantize, etc.)
brew install llama.cpp

# mlx-lm (provides mlx_lm.server — NOT mlx_lm.generate, which is a one-shot
# CLI and cannot serve HTTP requests)
uv tool install mlx-lm
```

Verify both are on `PATH`:
```bash
which llama-server mlx_lm.server
```

---

## Running llama-server (GGUF)

```bash
llama-server -m /path/to/model.gguf --port 8123 -c 4096
```

- `-c 4096` sets the context window; raise it if your system instructions +
  lexicon context + source text exceed 4096 tokens (the translate/neologism
  skills' `SKILL.md` system instructions are sizable).
- The `model` field in request bodies is **ignored** — one `llama-server`
  process always serves whichever GGUF was passed to `-m`.

**Example** (this project's fine-tuned Gemma 4 GGUF variants):
```bash
llama-server -m ~/projects/eldamo-tune/models/eldamo-gemma-q4_k_m.gguf --port 8123 -c 4096
```

**Verify it's serving OpenAI-compatible chat completions:**
```bash
curl -s http://127.0.0.1:8123/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"anything","stream":false,"messages":[{"role":"user","content":"Say hello in one short sentence."}]}'
```

---

## Running mlx_lm.server (MLX)

```bash
mlx_lm.server --model /path/to/mlx-model-dir --port 8124
```

**Example** (this project's fine-tuned Gemma 4 MLX weights, or a stock
Gemma 4 MLX checkpoint):
```bash
mlx_lm.server --model ~/projects/eldamo-tune/models/mlx --port 8124
# or a stock model:
mlx_lm.server --model ~/projects/gemma/mlx-gemma-4-e4b --port 8124
```

**Disabling "thinking" mode** — strongly recommended for the translate/
neologism skills (see [Gotcha 2](#gotcha-2-reasoning-mode-can-silently-eat-your-entire-token-budget) below):
```bash
mlx_lm.server --model ~/projects/eldamo-tune/models/mlx --port 8124 \
  --chat-template-args '{"enable_thinking": false}'
```

**Verify it's serving OpenAI-compatible chat completions:**
```bash
curl -s http://127.0.0.1:8124/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"default_model","stream":false,"messages":[{"role":"user","content":"Say hello in one short sentence."}]}'
```
(Note the `"model":"default_model"` — see Gotcha 1.)

---

## Wiring eldamo-server to whichever is running

```bash
export LOCAL_LLM_BASE_URL=http://localhost:8123   # :8123 for llama-server, :8124 for mlx_lm.server
export LOCAL_LLM_MODEL=default_model               # "default_model" is REQUIRED for mlx_lm.server; any value works for llama-server
export LOCAL_LLM_RUNTIME=llama.cpp                 # or "mlx" — tags usage records, no functional effect
export LOCAL_LLM_MAX_TOKENS=4096                   # raise further if using a reasoning-enabled model with thinking left on
make run-dev                                       # AUTH_BYPASS=true, for fast local iteration
```

Confirm the backend was picked up:
```
[A2A] Using local OpenAI-compatible LLM backend (base_url=http://localhost:8123, runtime=llama.cpp, model=default_model, max_tokens=4096)
```

Then exercise the skills with [`a2acli`](https://github.com/ghchinoy/a2acli):
```bash
a2acli send "translate to sindarin: the grey havens" --service-url http://localhost:8080 --wait
a2acli send "neologism starlight quenya" --service-url http://localhost:8080 --wait
```

---

## Gotchas

These were all discovered empirically while validating `eldamo-server-gut`
(Phase 3) against real local servers — not obvious from either project's
docs alone.

### Gotcha 1: the `model` field is *not* cosmetic in mlx_lm.server

`llama-server` ignores the request body's `"model"` field entirely — one
process serves one model, full stop. `mlx_lm.server`, however, treats any
`"model"` value other than `"default_model"` as a **Hugging Face repo ID to
fetch**, and will fail with a 404 (`Repository Not Found`) if you pass an
arbitrary string like your own model's filename.

**Fix:** always set `LOCAL_LLM_MODEL=default_model` when targeting
`mlx_lm.server`. (`mlx_lm.server` does support serving multiple models from
one process by loading additional repos on demand — out of scope here.)

### Gotcha 2: reasoning mode can silently eat your entire token budget

Some Gemma 4 checkpoints (including this project's fine-tuned variants) emit
a chain-of-thought "thinking" trace before their actual answer. On
`mlx_lm.server` this trace streams through a **non-standard `reasoning`
delta field**, separate from the standard OpenAI `content` field:

```jsonc
// reasoning tokens (not part of the OpenAI spec):
{"choices":[{"delta":{"reasoning":"Task: Say hello.\n*Constraint..."}}]}
// ...hundreds of these...
// eventual actual answer:
{"choices":[{"delta":{"content":"Hello!"}}]}
```

For the translate/neologism skills' long `SKILL.md`-based system
instructions, this reasoning trace can run long enough to consume the
**entire `max_tokens` budget** before any `content` is emitted —
`mlx_lm.server`'s own default is only 512 tokens. The result: a request that
returns `finish_reason: "length"` with an **empty final answer**, which
eldamo-server surfaces as "The LLM backend returned an empty response."

Observed impact: with thinking left on, a single translate/neologism
request against a small (~2-4B) local model took **minutes**; with
`--chat-template-args '{"enable_thinking": false}'`, the same request
completed in **seconds**.

**Fix (recommended):** disable thinking mode at server startup:
```bash
mlx_lm.server --model ... --chat-template-args '{"enable_thinking": false}'
```
**Fallback:** if you want to keep reasoning enabled (e.g. to inspect the
model's derivation logic), raise `LOCAL_LLM_MAX_TOKENS` well beyond the
default `4096` and expect multi-minute response times on modest hardware.

### Gotcha 3: `localhost` can resolve to a dead IPv6 address first

Both server binaries commonly bind IPv4-only (`127.0.0.1`), but `localhost`
often resolves to `::1` (IPv6 loopback) *first* on macOS. A naive
"connect to the first resolved IP" dialer gets `connection refused`.
eldamo-server's `localLLMHTTPClient()` (`llm_local.go`) already works around
this by trying every resolved IP in order — this is called out here only so
you recognize the symptom (`dial tcp [::1]:PORT: connect: connection
refused`) if you ever see it while debugging outside eldamo-server, e.g.
with a different HTTP client or language.

---

## Switching models

Both `llama-server` and `mlx_lm.server` load **one model per process**.
There is no live model-switching via the `LOCAL_LLM_MODEL` env var or the
request body (aside from mlx_lm.server's HF-repo-fetch behavior in Gotcha 1,
which is not what you want for local files). To compare models:

- **Stop and restart** the server with a different `-m`/`--model` flag, or
- **Run multiple instances on different ports** and flip
  `LOCAL_LLM_BASE_URL` between them, or
- For llama.cpp specifically, front multiple models with
  [`llama-swap`](https://github.com/mostlygeek/llama-swap) for hot-swap-by-model-name
  without restarting or juggling ports.
