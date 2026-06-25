package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	analysisdomain "github.com/kanitin/stackvest/backend/internal/domain/analysis"
	analysisuc "github.com/kanitin/stackvest/backend/internal/usecase/analysis"
)

type mockStreamer struct {
	body string
	err  error
}

func (m *mockStreamer) StreamChat(_ context.Context, _, _ string) (io.ReadCloser, error) {
	if m.err != nil {
		return nil, m.err
	}
	return io.NopCloser(strings.NewReader(m.body)), nil
}

func newAnalyzeRouter(s analysisdomain.Streamer) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewPortfolioHandler(nil, analysisuc.New(s)).RegisterRoutes(r.Group(""))
	return r
}

const validAnalyzeBody = `{"portfolio":{"name":"Dividend portfolio","holdings":[
	{"ticker":"JEPI","actual":42,"target":34}]},
	"dimensions":["concentration risk"]}`

func TestAnalyze(t *testing.T) {
	// Upstream SSE with two data frames plus its own terminal [DONE].
	upstream := "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n\n" +
		"data: [DONE]\n\n"

	tests := []struct {
		name     string
		body     string
		streamer *mockStreamer
		wantCode int
		wantCT   string
	}{
		{"invalid body", `{`, &mockStreamer{body: upstream}, http.StatusBadRequest, ""},
		{"empty holdings", `{"portfolio":{"name":"x","holdings":[]},"dimensions":["risk"]}`, &mockStreamer{body: upstream}, http.StatusBadRequest, ""},
		{"empty dimensions", `{"portfolio":{"name":"x","holdings":[{"ticker":"JEPI","actual":42,"target":34}]},"dimensions":[]}`, &mockStreamer{body: upstream}, http.StatusBadRequest, ""},
		{"missing name", `{"portfolio":{"holdings":[{"ticker":"JEPI","actual":42,"target":34}]},"dimensions":["risk"]}`, &mockStreamer{body: upstream}, http.StatusBadRequest, ""},
		{"blank ticker", `{"portfolio":{"name":"x","holdings":[{"ticker":"","actual":42,"target":34}]},"dimensions":["risk"]}`, &mockStreamer{body: upstream}, http.StatusBadRequest, ""},
		{"negative weight", `{"portfolio":{"name":"x","holdings":[{"ticker":"JEPI","actual":-1,"target":34}]},"dimensions":["risk"]}`, &mockStreamer{body: upstream}, http.StatusBadRequest, ""},
		{"blank dimension", `{"portfolio":{"name":"x","holdings":[{"ticker":"JEPI","actual":42,"target":34}]},"dimensions":[""]}`, &mockStreamer{body: upstream}, http.StatusBadRequest, ""},
		{"rate limited", validAnalyzeBody, &mockStreamer{err: analysisdomain.ErrRateLimited}, http.StatusTooManyRequests, ""},
		{"upstream error", validAnalyzeBody, &mockStreamer{err: analysisdomain.ErrUpstream}, http.StatusBadGateway, ""},
		{"success", validAnalyzeBody, &mockStreamer{body: upstream}, http.StatusOK, "text/event-stream"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newAnalyzeRouter(tc.streamer)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/portfolios/analyze", strings.NewReader(tc.body))
			r.ServeHTTP(w, req)

			if w.Code != tc.wantCode {
				t.Fatalf("expected status %d, got %d", tc.wantCode, w.Code)
			}
			if tc.wantCT != "" && !strings.Contains(w.Header().Get("Content-Type"), tc.wantCT) {
				t.Fatalf("expected Content-Type %q, got %q", tc.wantCT, w.Header().Get("Content-Type"))
			}

			if tc.name == "success" {
				out := w.Body.String()
				if !strings.Contains(out, "Hello") || !strings.Contains(out, "world") {
					t.Fatalf("forwarded chunks missing in output: %q", out)
				}
				if n := strings.Count(out, "data: [DONE]"); n != 1 {
					t.Fatalf("expected exactly one [DONE], got %d in %q", n, out)
				}
				if strings.Contains(out, "\n\n\n") {
					t.Fatalf("stray blank line in SSE output: %q", out)
				}
			}
		})
	}
}
