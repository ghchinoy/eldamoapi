# Model Evaluation: Comparing LLM Backends for translate/neologism

This is a **living document**, not a pass/fail test plan — see
[test-plan.md §3](test-plan.md#3-local-llm-backend-testing-gemma-4-via-llamacpp--mlx_lmserver)
for functional/regression verification of the local backend plumbing. This
doc tracks *response quality vs. cost/latency* across:

- **Backends:** Vertex AI Gemini (hosted), llama.cpp (local GGUF), mlx_lm.server (local MLX)
- **Model variants:** stock Gemma 4 (E2B/E4B/12B) vs. this project's fine-tuned `eldamo-gemma` checkpoints, across quantization levels (`q4_0`, `q4_k_m`, etc.)

The goal, per `eldamo-server-hk1` (Gemma 4 epic): decide whether a hosted
Gemma 4 deployment (`eldamo-server-3k0`, Phase 5, currently backlog) is
worth the infrastructure cost, by comparing it against what local inference
already provides.

---

## Why a separate doc from test-plan.md

| | test-plan.md | this doc |
| :--- | :--- | :--- |
| Question answered | "Does it work?" | "Which is *better*, and by how much?" |
| Output | PASS / FAIL | Quality notes, tokens, latency, $ cost |
| Cadence | Run on every relevant change | Run periodically / when comparing new variants |
| Blocking? | Yes — CI gate | No — informational, drives decisions |

## Reading usage from server logs (Phase 2 — live)

`eldamo-server-9zq` (Phase 2) is complete. Every translate/neologism call
now emits a structured `[Usage]` log line on the server's stderr — readable
from Cloud Run logs, `go run .` stdout, or any log drain:

```
[Usage] skill=translate user="bypass-user" status=ok backend=llama.cpp model=eldamo-gemma prompt_tokens=1225 completion_tokens=26 total_tokens=1251 latency=1.131s cost_usd=0.000000
```

Fields:

| Field | Description |
| :--- | :--- |
| `skill` | `translate` or `neologism` |
| `user` | JWT `sub` claim (or `bypass-user` in AUTH_BYPASS mode) |
| `status` | `ok` or `error` |
| `backend` | Matches `LOCAL_LLM_RUNTIME` (e.g. `llama.cpp`, `mlx`) or `vertex-gemini` |
| `model` | Model identifier passed to the backend |
| `prompt_tokens` / `completion_tokens` / `total_tokens` | From the backend's `usage` response field |
| `latency` | Wall-clock stream duration |
| `cost_usd` | Estimated USD from `skills.EstimateCostUSD` — `0` for local backends; computed from a $/1M-token table for known hosted models (currently `gemini-3.1-flash-lite`; extend in `skills/usage.go:knownPricing` as new hosted models are added) |
| `err=` | Present on `status=error` only — the backend error message |

To populate the results table below, run the fixed prompt set against each
model and capture the usage lines:

```bash
# Point your server at a backend, then run prompts and grep the usage lines:
grep '\[Usage\]' /path/to/server.log
```

---

## Fixed prompt set

Use the same prompts across every backend/model comparison run so results
are apples-to-apples. Extend this list as needed, but keep it stable once a
comparison series has started.

**Translate:**
1. `translate to quenya: a star shines`
2. `translate to sindarin: the grey havens`
3. `translate farewell my friend to quenya`

**Neologism:**
1. `neologism starlight quenya`
2. `coin a word for artificial intelligence sindarin`
3. `invent: blockchain in quenya`

---

## Model / backend inventory

Fill in with what's actually available on the test machine. Example
(replace with your own paths/hardware):

| Label | Backend | Runtime | Format | Path | Notes |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `gemini-flash-lite` | Vertex AI | cloud | — | `GEMINI_TRANSLATE_MODEL=gemini-3.1-flash-lite` | Baseline / current production |
| `stock-e2b-gguf` | llama.cpp | local | GGUF | `~/projects/gemma/gemma-4-E2B-it-Q4_K_M.gguf` | Stock Gemma 4, smallest |
| `stock-e4b-gguf` | llama.cpp | local | GGUF | `~/projects/gemma/gemma-4-E4B-it-Q4_K_M.gguf` | Stock Gemma 4 |
| `tuned-gguf-q4km` | llama.cpp | local | GGUF | `~/projects/eldamo-tune/models/eldamo-gemma-q4_k_m.gguf` | Fine-tuned, most compressed |
| `tuned-gguf-12b` | llama.cpp | local | GGUF | `~/projects/eldamo-tune/models/eldamo-gemma-12b-q4_0.gguf` | Fine-tuned, largest |
| `stock-e4b-mlx` | mlx_lm.server | local | MLX | `~/projects/gemma/mlx-gemma-4-e4b` | Stock Gemma 4, Apple Silicon |
| `tuned-mlx` | mlx_lm.server | local | MLX | `~/projects/eldamo-tune/models/mlx` | Fine-tuned, Apple Silicon |

---

## Results log

One row per (prompt, model) run. `enable_thinking` only applies to
mlx_lm.server; note it explicitly since it drastically affects latency (see
[local-llm-servers.md Gotcha 2](local-llm-servers.md#gotcha-2-reasoning-mode-can-silently-eat-your-entire-token-budget)).

| Date | Prompt | Model label | `enable_thinking` | Prompt tokens | Completion tokens | Latency | $ cost | Quality notes |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| | | | | | | | | |

**Quality notes should cover, at minimum:**
- Did it follow the skill's required output format (e.g. `PracticalDelim`/`PoeticDelim` section headers for neologism)?
- Linguistic correctness (as far as assessable) — did it use/acknowledge the lexicon context provided in the prompt?
- Any hallucination, refusal, or off-task chatter?

---

## Known findings so far

_(Update as comparisons accumulate. Seed entries from Phase 3 validation
testing — timing was incidental to plumbing verification, not a rigorous
benchmark, so treat as directional only.)_

- **`enable_thinking` matters enormously for mlx_lm.server latency.** With
  reasoning left on, a `tuned-mlx` translate call against the full
  `SKILL.md` system instruction took multiple minutes and sometimes
  exhausted the token budget before producing any answer (empty response).
  With `--chat-template-args '{"enable_thinking": false}'`, the same request
  completed in seconds. **Recommendation: always disable thinking mode for
  these skills** unless specifically evaluating reasoning-trace quality.
- **llama.cpp GGUF inference was fast and reliable** for the tuned
  `q4_k_m` checkpoint even with reasoning-style output present in some
  responses — no token-budget exhaustion observed in initial smoke testing
  (default 512-token llama-server budget was not hit, unlike mlx_lm.server's
  same default).

---

## Next steps

- [ ] Populate the model inventory table with the actual local files present on this machine.
- [ ] Run the fixed prompt set against each `(model, backend)` pair; log results.
- [x] Phase 2 (`eldamo-server-9zq`) done — `[Usage]` log lines emitted per call with backend/model/tokens/latency/cost. Grep server logs to fill in results table.
- [ ] Once enough data exists, revisit `eldamo-server-3k0` (Phase 5, hosted Gemma) — is hosting worth it vs. local MLX/llama.cpp for this workload?
