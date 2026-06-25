package groq

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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
	if gotBody.Model != model || gotBody.MaxTokens != maxTokens || !gotBody.Stream {
		t.Errorf("body = %+v", gotBody)
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

func TestStreamChat_StatusMapping(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		wantErr error
		wantOK  bool
	}{
		{"ok", http.StatusOK, nil, true},
		{"rate limited", http.StatusTooManyRequests, analysis.ErrRateLimited, false},
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
