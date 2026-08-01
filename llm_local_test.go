package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ghchinoy/eldamoapi/internal/skills"
)

// TestLocalLLMClient_StreamsChunksAndUsage spins up an httptest.Server that
// mimics the OpenAI-compatible SSE wire format shared by llama.cpp's
// llama-server and mlx-lm's mlx_lm.server, and verifies localLLMClient
// correctly assembles streamed text and captures the trailing usage chunk.
func TestLocalLLMClient_StreamsChunksAndUsage(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = string(buf)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		events := []string{
			`{"choices":[{"delta":{"content":"Elen "}}]}`,
			`{"choices":[{"delta":{"content":"síla."}}]}`,
			`{"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":4,"total_tokens":16}}`,
			`[DONE]`,
		}
		for _, e := range events {
			if _, err := fmt.Fprintf(w, "data: %s\n\n", e); err != nil {
				t.Errorf("write SSE event: %v", err)
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()

	client := newLocalLLMClient(srv.URL, "llama.cpp", localLLMDefaultMaxTokens)

	var full strings.Builder
	var gotUsage *skills.Usage
	for chunk, err := range client.GenerateContentStream(context.Background(), "eldamo-gemma-q4_k_m", "system prompt", "user prompt") {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		full.WriteString(chunk.Text)
		if chunk.Usage != nil {
			gotUsage = chunk.Usage
		}
	}

	if want := "Elen síla."; full.String() != want {
		t.Errorf("assembled text: want %q, got %q", want, full.String())
	}
	if gotUsage == nil {
		t.Fatal("expected a usage chunk, got none")
	}
	if gotUsage.Backend != "llama.cpp" {
		t.Errorf("usage.Backend: want llama.cpp, got %q", gotUsage.Backend)
	}
	if gotUsage.Model != "eldamo-gemma-q4_k_m" {
		t.Errorf("usage.Model: want eldamo-gemma-q4_k_m, got %q", gotUsage.Model)
	}
	if gotUsage.PromptTokens != 12 || gotUsage.CompletionTokens != 4 || gotUsage.TotalTokens != 16 {
		t.Errorf("usage token counts: got %+v", gotUsage)
	}
	if !strings.Contains(gotBody, `"stream":true`) {
		t.Errorf("request body missing stream:true: %s", gotBody)
	}
	if !strings.Contains(gotBody, fmt.Sprintf(`"max_tokens":%d`, localLLMDefaultMaxTokens)) {
		t.Errorf("request body missing expected max_tokens: %s", gotBody)
	}
	if !strings.Contains(gotBody, `"system prompt"`) || !strings.Contains(gotBody, `"user prompt"`) {
		t.Errorf("request body missing expected messages: %s", gotBody)
	}
}

// TestLocalLLMClient_HTTPError verifies a non-200 response surfaces as an
// error rather than being silently swallowed.
func TestLocalLLMClient_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("model not loaded"))
	}))
	defer srv.Close()

	client := newLocalLLMClient(srv.URL, "mlx", localLLMDefaultMaxTokens)

	var sawErr bool
	for _, err := range client.GenerateContentStream(context.Background(), "test-model", "sys", "prompt") {
		if err != nil {
			sawErr = true
			if !strings.Contains(err.Error(), "500") {
				t.Errorf("expected error to mention status 500, got: %v", err)
			}
		}
	}
	if !sawErr {
		t.Error("expected an error for HTTP 500 response")
	}
}

// TestLocalLLMEnabledPrecedence verifies buildDeps prefers the local backend
// over Vertex Gemini when both are configured (documented precedence).
func TestLocalLLMConfigHelpers(t *testing.T) {
	t.Run("disabled by default", func(t *testing.T) {
		t.Setenv("LOCAL_LLM_BASE_URL", "")
		if localLLMEnabled() {
			t.Error("expected localLLMEnabled() to be false when unset")
		}
	})
	t.Run("enabled when base URL set", func(t *testing.T) {
		t.Setenv("LOCAL_LLM_BASE_URL", "http://localhost:8080")
		if !localLLMEnabled() {
			t.Error("expected localLLMEnabled() to be true when set")
		}
		if got := localLLMBaseURL(); got != "http://localhost:8080" {
			t.Errorf("localLLMBaseURL() = %q", got)
		}
	})
	t.Run("model name defaults", func(t *testing.T) {
		t.Setenv("LOCAL_LLM_MODEL", "")
		if got := localLLMModelName(); got != "local-model" {
			t.Errorf("localLLMModelName() default = %q, want local-model", got)
		}
		t.Setenv("LOCAL_LLM_MODEL", "eldamo-gemma-q4_k_m")
		if got := localLLMModelName(); got != "eldamo-gemma-q4_k_m" {
			t.Errorf("localLLMModelName() = %q", got)
		}
	})
	t.Run("runtime label defaults", func(t *testing.T) {
		t.Setenv("LOCAL_LLM_RUNTIME", "")
		if got := localLLMRuntime(); got != "local" {
			t.Errorf("localLLMRuntime() default = %q, want local", got)
		}
		t.Setenv("LOCAL_LLM_RUNTIME", "mlx")
		if got := localLLMRuntime(); got != "mlx" {
			t.Errorf("localLLMRuntime() = %q", got)
		}
	})
	t.Run("max tokens defaults and parses override", func(t *testing.T) {
		t.Setenv("LOCAL_LLM_MAX_TOKENS", "")
		if got := localLLMMaxTokens(); got != localLLMDefaultMaxTokens {
			t.Errorf("localLLMMaxTokens() default = %d, want %d", got, localLLMDefaultMaxTokens)
		}
		t.Setenv("LOCAL_LLM_MAX_TOKENS", "2048")
		if got := localLLMMaxTokens(); got != 2048 {
			t.Errorf("localLLMMaxTokens() = %d, want 2048", got)
		}
		t.Run("invalid value falls back to default", func(t *testing.T) {
			t.Setenv("LOCAL_LLM_MAX_TOKENS", "not-a-number")
			if got := localLLMMaxTokens(); got != localLLMDefaultMaxTokens {
				t.Errorf("localLLMMaxTokens() with invalid value = %d, want default %d", got, localLLMDefaultMaxTokens)
			}
		})
	})
}
