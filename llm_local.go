package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ghchinoy/eldamoapi/internal/skills"
)

// ── Local OpenAI-compatible LLM backend ─────────────────────────────────────
//
// This adapts any server speaking the OpenAI /v1/chat/completions streaming
// protocol to skills.LLMClient. Both llama.cpp's llama-server (GGUF models)
// and mlx-lm's mlx_lm.server (MLX models, Apple Silicon acceleration)
// implement this identical wire format — confirmed by inspecting
// mlx_lm/server.py, which routes /v1/chat/completions and /v1/completions
// with SSE streaming and a standard usage{prompt_tokens,completion_tokens}
// block, matching llama.cpp's server. One client therefore serves both
// runtimes; LOCAL_LLM_RUNTIME is purely a label for usage/cost tracking
// (Phase 2) so comparisons can break local inference down by runtime.
//
// Note: mlx_lm.generate is a one-shot CLI (not a server); the correct
// utility to run alongside this client is mlx_lm.server.

// localLLMEnabled reports whether the local OpenAI-compatible backend is
// configured. When set, it takes precedence over GEMINI_TRANSLATE_MODEL (see
// buildDeps) so a developer can point at a local llama-server/mlx_lm.server
// without having to unset their Vertex AI config.
func localLLMEnabled() bool {
	return os.Getenv("LOCAL_LLM_BASE_URL") != ""
}

// localLLMBaseURL returns the configured base URL (e.g. "http://localhost:8080"),
// with no path suffix — "/v1/chat/completions" is appended per-request.
func localLLMBaseURL() string {
	return strings.TrimRight(os.Getenv("LOCAL_LLM_BASE_URL"), "/")
}

// localLLMModelName returns the model identifier to send in each request.
// llama-server and mlx_lm.server each serve a single model per process, so
// this is mostly informational/logging — it does not select which weights
// respond — but it's still required by the OpenAI request schema and is
// recorded in skills.Usage for cost/quality comparisons.
func localLLMModelName() string {
	if m := os.Getenv("LOCAL_LLM_MODEL"); m != "" {
		return m
	}
	return "local-model"
}

// localLLMRuntime returns the runtime label used to tag skills.Usage.Backend,
// e.g. "llama.cpp" or "mlx", distinguishing which local server actually
// produced a given response for later cost/quality comparisons.
func localLLMRuntime() string {
	if r := os.Getenv("LOCAL_LLM_RUNTIME"); r != "" {
		return r
	}
	return "local"
}

// localLLMDefaultMaxTokens is used when LOCAL_LLM_MAX_TOKENS is unset.
//
// This is deliberately much higher than mlx_lm.server's own default (512):
// reasoning-enabled Gemma checkpoints (confirmed empirically against
// mlx_lm.server 0.31.x serving a Gemma 4 MLX model) emit their chain-of-
// thought as a separate "reasoning" delta field before any "content" delta.
// With the long system instructions used by the translate/neologism skills,
// 512 tokens can be consumed entirely by reasoning (finish_reason: "length")
// before a single content token is produced, yielding an empty response.
const localLLMDefaultMaxTokens = 4096

// localLLMMaxTokens returns the max_tokens value to send in each request.
func localLLMMaxTokens() int {
	if v := os.Getenv("LOCAL_LLM_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
		log.Printf("[A2A] invalid LOCAL_LLM_MAX_TOKENS=%q; using default %d", v, localLLMDefaultMaxTokens)
	}
	return localLLMDefaultMaxTokens
}

// localLLMHTTPClient returns an http.Client for talking to an operator-
// configured local LLM server.
//
// This deliberately does NOT reuse SafeHTTPClient(): that dialer blocks
// loopback/private IPs because it protects against SSRF via arbitrary,
// per-request, potentially attacker-influenced URLs (CIMD client_id
// discovery, OAuth redirects). LOCAL_LLM_BASE_URL has a different trust
// model — it is a fixed value the operator sets once at process startup
// (the same trust level as GCP_PROJECT/GEMINI_LOCATION), and it is *expected*
// to point at localhost or a private LAN address running llama-server or
// mlx_lm.server. Blocking private IPs here would defeat the feature's
// purpose. This client keeps the same resolve-then-dial-to-resolved-IP
// pattern (avoiding DNS-rebinding TOCTOU) and a bounded redirect count, but
// does not reject private/loopback ranges.
//
// No client-level Timeout is set: local generation (especially CPU-bound
// GGUF/MLX inference of larger models) can legitimately run well past
// typical HTTP timeouts, and http.Client.Timeout would otherwise truncate
// the SSE stream mid-response. Callers bound overall duration via ctx.
func localLLMHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
				if err != nil {
					return nil, err
				}
				if len(ips) == 0 {
					return nil, fmt.Errorf("no addresses found for host: %s", host)
				}
				// Try each resolved IP in order (e.g. "localhost" commonly
				// resolves to both ::1 and 127.0.0.1, but local LLM servers
				// like llama-server/mlx_lm.server often bind IPv4-only) —
				// mirrors Go's default Happy-Eyeballs-ish dial fallback,
				// which the single-IP dial in SafeHTTPClient() lacks.
				dialer := net.Dialer{Timeout: 5 * time.Second}
				var lastErr error
				for _, ip := range ips {
					conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
					if err == nil {
						return conn, nil
					}
					lastErr = err
				}
				return nil, lastErr
			},
		},
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
}

// ── OpenAI chat-completions wire types (subset) ─────────────────────────────

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type openAIChatRequest struct {
	Model         string               `json:"model"`
	Messages      []openAIChatMessage  `json:"messages"`
	Stream        bool                 `json:"stream"`
	StreamOptions *openAIStreamOptions `json:"stream_options,omitempty"`
	MaxTokens     int                  `json:"max_tokens,omitempty"`
}

type openAIChoiceDelta struct {
	Content string `json:"content"`
}

type openAIChoice struct {
	Delta openAIChoiceDelta `json:"delta"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openAIChatStreamChunk struct {
	Choices []openAIChoice `json:"choices"`
	Usage   *openAIUsage   `json:"usage"`
}

// ── localLLMClient ───────────────────────────────────────────────────────────

// localLLMClient adapts an OpenAI-compatible /v1/chat/completions streaming
// endpoint (llama-server or mlx_lm.server) to skills.LLMClient.
type localLLMClient struct {
	baseURL    string
	runtime    string
	maxTokens  int
	httpClient *http.Client
}

var _ skills.LLMClient = (*localLLMClient)(nil)

// newLocalLLMClient constructs a localLLMClient targeting baseURL (e.g.
// "http://localhost:8080"), tagging usage records with runtime (e.g.
// "llama.cpp" or "mlx"), and capping generations at maxTokens (see
// localLLMDefaultMaxTokens for why this needs to be generous for
// reasoning-enabled local models).
func newLocalLLMClient(baseURL, runtime string, maxTokens int) *localLLMClient {
	return &localLLMClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		runtime:    runtime,
		maxTokens:  maxTokens,
		httpClient: localLLMHTTPClient(),
	}
}

func (l *localLLMClient) GenerateContentStream(ctx context.Context, model, systemInstruction, prompt string) iter.Seq2[skills.GenChunk, error] {
	return func(yield func(skills.GenChunk, error) bool) {
		reqBody := openAIChatRequest{
			Model:         model,
			Stream:        true,
			StreamOptions: &openAIStreamOptions{IncludeUsage: true},
			MaxTokens:     l.maxTokens,
			Messages: []openAIChatMessage{
				{Role: "system", Content: systemInstruction},
				{Role: "user", Content: prompt},
			},
		}
		body, err := json.Marshal(reqBody)
		if err != nil {
			yield(skills.GenChunk{}, fmt.Errorf("local LLM (%s): marshal request: %w", l.runtime, err))
			return
		}

		url := l.baseURL + "/v1/chat/completions"
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			yield(skills.GenChunk{}, fmt.Errorf("local LLM (%s): build request: %w", l.runtime, err))
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")

		resp, err := l.httpClient.Do(httpReq)
		if err != nil {
			yield(skills.GenChunk{}, fmt.Errorf("local LLM (%s): request to %s failed: %w", l.runtime, url, err))
			return
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			yield(skills.GenChunk{}, fmt.Errorf("local LLM (%s): unexpected status %d from %s: %s",
				l.runtime, resp.StatusCode, url, strings.TrimSpace(string(b))))
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				break
			}

			var chunk openAIChatStreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				yield(skills.GenChunk{}, fmt.Errorf("local LLM (%s): decode stream chunk: %w", l.runtime, err))
				return
			}

			var text string
			if len(chunk.Choices) > 0 {
				text = chunk.Choices[0].Delta.Content
			}
			var usage *skills.Usage
			if chunk.Usage != nil {
				usage = &skills.Usage{
					Backend:          l.runtime,
					Model:            model,
					PromptTokens:     chunk.Usage.PromptTokens,
					CompletionTokens: chunk.Usage.CompletionTokens,
					TotalTokens:      chunk.Usage.TotalTokens,
				}
			}
			if text == "" && usage == nil {
				continue
			}
			if !yield(skills.GenChunk{Text: text, Usage: usage}, nil) {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			yield(skills.GenChunk{}, fmt.Errorf("local LLM (%s): reading stream: %w", l.runtime, err))
			return
		}
		log.Printf("[A2A] local LLM (%s) stream complete (base_url=%s, model=%s)", l.runtime, l.baseURL, model)
	}
}
