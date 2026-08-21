package groq

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/kanitin/stackvest/backend/internal/domain/analysis"
)

func newTestClient(srv *httptest.Server) *Client {
	c := NewClient("test-key")
	c.baseURL = srv.URL
	return c
}

func TestStreamChat_RequestShape(t *testing.T) {
	var gotAuth, gotCT, gotMethod string
	var gotBody chatRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	body, err := newTestClient(srv).StreamChat(context.Background(), "system instructions", "hello prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body.Close()

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q", gotCT)
	}
	if gotBody.Model != models[0].id {
		t.Errorf("model = %q, want first of chain %q", gotBody.Model, models[0].id)
	}
	if gotBody.MaxCompletionTokens != maxCompletionTokens || !gotBody.Stream {
		t.Errorf("body = %+v", gotBody)
	}
	// Reasoning must be suppressed: the raw SSE stream is forwarded to the browser
	// and the analysis prompt expects a bare JSON object.
	if gotBody.ReasoningFormat != reasoningFormat {
		t.Errorf("reasoning_format = %q, want %q", gotBody.ReasoningFormat, reasoningFormat)
	}
	if gotBody.ReasoningEffort != models[0].reasoningEffort {
		t.Errorf("reasoning_effort = %q, want %q", gotBody.ReasoningEffort, models[0].reasoningEffort)
	}
	if len(gotBody.Messages) != 2 {
		t.Fatalf("messages = %+v, want 2", gotBody.Messages)
	}
	if gotBody.Messages[0].Role != "system" || gotBody.Messages[0].Content != "system instructions" {
		t.Errorf("system message = %+v", gotBody.Messages[0])
	}
	if gotBody.Messages[1].Role != "user" || gotBody.Messages[1].Content != "hello prompt" {
		t.Errorf("user message = %+v", gotBody.Messages[1])
	}
}

// recordingServer replies with the given statuses in order, one per request, and
// records what each request actually asked for.
func recordingServer(t *testing.T, statuses ...int) (*httptest.Server, func() []chatRequest) {
	t.Helper()
	var mu sync.Mutex
	var seen []chatRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		mu.Lock()
		n := len(seen)
		seen = append(seen, req)
		mu.Unlock()

		status := http.StatusInternalServerError
		if n < len(statuses) {
			status = statuses[n]
		}
		w.WriteHeader(status)
		io.WriteString(w, "data: ok\n\n")
	}))
	t.Cleanup(srv.Close)

	return srv, func() []chatRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]chatRequest(nil), seen...)
	}
}

// modelsOf reduces recorded requests to the model ids, in order.
func modelsOf(reqs []chatRequest) []string {
	ids := make([]string, len(reqs))
	for i, r := range reqs {
		ids[i] = r.Model
	}
	return ids
}

func TestStreamChat_FallsBackToNextModel(t *testing.T) {
	// First model decommissioned upstream — exactly the 404 that motivated the chain.
	srv, requests := recordingServer(t, http.StatusNotFound, http.StatusOK)

	body, err := newTestClient(srv).StreamChat(context.Background(), "sys", "p")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer body.Close()

	got := modelsOf(requests())
	if len(got) != 2 {
		t.Fatalf("requests = %v, want 2", got)
	}
	if got[0] != models[0].id || got[1] != models[1].id {
		t.Errorf("tried %v, want [%s %s]", got, models[0].id, models[1].id)
	}

	// The stream handed back must be the one that succeeded.
	streamed, _ := io.ReadAll(body)
	if string(streamed) != "data: ok\n\n" {
		t.Errorf("streamed %q, want the second attempt's body", streamed)
	}
}

func TestStreamChat_FallsBackTwice(t *testing.T) {
	srv, requests := recordingServer(t, http.StatusNotFound, http.StatusNotFound, http.StatusOK)

	body, err := newTestClient(srv).StreamChat(context.Background(), "sys", "p")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body.Close()

	got := requests()
	if len(got) != 3 || got[2].Model != models[2].id {
		t.Fatalf("tried %v, want the third model %q last", modelsOf(got), models[2].id)
	}

	// Each request must carry the reasoning_effort its own model family accepts —
	// gpt-oss takes low/medium/high, qwen3 takes none/default. Sending gpt-oss's
	// value to the qwen fallback would 400 at the worst possible moment: when both
	// models ahead of it are already down.
	for i, req := range got {
		if req.Model != models[i].id {
			t.Fatalf("request %d used model %q, want %q", i, req.Model, models[i].id)
		}
		if req.ReasoningEffort != models[i].reasoningEffort {
			t.Errorf("request %d (%s) sent reasoning_effort=%q, want %q",
				i, req.Model, req.ReasoningEffort, models[i].reasoningEffort)
		}
		if req.ReasoningFormat != reasoningFormat {
			t.Errorf("request %d (%s) sent reasoning_format=%q, want %q",
				i, req.Model, req.ReasoningFormat, reasoningFormat)
		}
	}
}

func TestStreamChat_AuthFailureDoesNotFanOut(t *testing.T) {
	// One key serves the whole chain, so 401 must stop at the first model rather
	// than burning a round trip per model on the same rejection.
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		srv, requests := recordingServer(t, status, http.StatusOK, http.StatusOK)

		body, err := newTestClient(srv).StreamChat(context.Background(), "sys", "p")
		if body != nil {
			t.Fatalf("status %d: expected nil body", status)
		}
		if !errors.Is(err, analysis.ErrUpstream) {
			t.Fatalf("status %d: error = %v, want ErrUpstream", status, err)
		}
		if got := requests(); len(got) != 1 {
			t.Errorf("status %d: made %d requests, want 1", status, len(got))
		}
	}
}

func TestStreamChat_UnrecoverableRequestErrorAbortsChain(t *testing.T) {
	// A request that cannot even be built is deterministic — the next model would
	// fail identically — so the chain must stop rather than retry it three times.
	c := NewClient("test-key")
	c.baseURL = "http:// invalid"

	body, err := c.StreamChat(context.Background(), "sys", "p")
	if body != nil {
		t.Fatalf("expected nil body")
	}
	if !errors.Is(err, analysis.ErrUpstream) {
		t.Fatalf("error = %v, want ErrUpstream", err)
	}
	if strings.Contains(err.Error(), models[1].id) {
		t.Errorf("error %q names a later model; the chain should have aborted at the first", err)
	}
}

func TestStreamChat_AllRateLimited(t *testing.T) {
	srv, requests := recordingServer(t, http.StatusTooManyRequests, http.StatusTooManyRequests, http.StatusTooManyRequests)

	body, err := newTestClient(srv).StreamChat(context.Background(), "sys", "p")
	if body != nil {
		t.Fatalf("expected nil body")
	}
	if !errors.Is(err, analysis.ErrRateLimited) {
		t.Fatalf("error = %v, want ErrRateLimited", err)
	}
	if got := requests(); len(got) != len(models) {
		t.Errorf("made %d requests, want %d", len(got), len(models))
	}
}

func TestStreamChat_PartialRateLimitIsUpstreamError(t *testing.T) {
	// A 429 mixed with a hard failure is not a "retry later" situation.
	srv, _ := recordingServer(t, http.StatusTooManyRequests, http.StatusNotFound, http.StatusTooManyRequests)

	_, err := newTestClient(srv).StreamChat(context.Background(), "sys", "p")
	if errors.Is(err, analysis.ErrRateLimited) {
		t.Fatalf("error = %v, want ErrUpstream not ErrRateLimited", err)
	}
	if !errors.Is(err, analysis.ErrUpstream) {
		t.Fatalf("error = %v, want ErrUpstream", err)
	}
}

func TestStreamChat_ExhaustedErrorNamesEveryAttempt(t *testing.T) {
	srv, _ := recordingServer(t, http.StatusNotFound, http.StatusInternalServerError, http.StatusBadRequest)

	_, err := newTestClient(srv).StreamChat(context.Background(), "sys", "p")
	if !errors.Is(err, analysis.ErrUpstream) {
		t.Fatalf("error = %v, want ErrUpstream", err)
	}
	// The whole point of aggregating: the last model's error alone would point at
	// the wrong model when the chain is misconfigured.
	for _, m := range models {
		if !strings.Contains(err.Error(), m.id) {
			t.Errorf("error %q does not name model %q", err, m.id)
		}
	}
	for _, status := range []string{"404", "500", "400"} {
		if !strings.Contains(err.Error(), status) {
			t.Errorf("error %q does not report status %s", err, status)
		}
	}
}

func TestStreamChat_CanceledContextStopsChain(t *testing.T) {
	srv, requests := recordingServer(t, http.StatusNotFound, http.StatusOK)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	body, err := newTestClient(srv).StreamChat(ctx, "sys", "p")
	if body != nil {
		t.Fatalf("expected nil body")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want it to wrap context.Canceled", err)
	}
	if !errors.Is(err, analysis.ErrUpstream) {
		t.Errorf("error = %v, want it to wrap ErrUpstream", err)
	}
	if got := requests(); len(got) != 0 {
		t.Errorf("made %d requests on a canceled context, want 0", len(got))
	}
}

func TestStreamChat_StatusMapping(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		wantErr error
		wantOK  bool
	}{
		{"ok", http.StatusOK, nil, true},
		{"server error", http.StatusInternalServerError, analysis.ErrUpstream, false},
		{"bad request", http.StatusBadRequest, analysis.ErrUpstream, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				io.WriteString(w, "data: ok\n\n")
			}))
			defer srv.Close()

			body, err := newTestClient(srv).StreamChat(context.Background(), "sys", "p")
			if tc.wantOK {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				body.Close()
				return
			}
			if body != nil {
				t.Fatalf("expected nil body on error")
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
