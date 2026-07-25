package groq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/kanitin/stackvest/backend/internal/domain/analysis"
)

const (
	model     = "llama-3.3-70b-versatile"
	maxTokens = 400
)

// Client calls Groq's OpenAI-compatible chat completions API.
type Client struct {
	apiKey     string
	httpClient *http.Client
	baseURL    string
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		// Deliberately no blanket http.Client.Timeout: StreamChat forwards a live SSE
		// stream, and Timeout bounds the entire request including body read, which
		// would silently truncate a legitimate long-running analysis. These
		// transport-level timeouts only bound "stuck before any data arrives";
		// cancellation of an in-flight stream is left to the caller's own ctx
		// (already threaded through via http.NewRequestWithContext below).
		httpClient: &http.Client{
			Transport: &http.Transport{
				DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 20 * time.Second,
			},
		},
		baseURL: "https://api.groq.com/openai/v1/chat/completions",
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model     string        `json:"model"`
	MaxTokens int           `json:"max_tokens"`
	Stream    bool          `json:"stream"`
	Messages  []chatMessage `json:"messages"`
}

// StreamChat implements analysis.Streamer. It POSTs the prompt with stream=true and
// returns the raw SSE response body for the caller to forward. On 429 it returns
// analysis.ErrRateLimited; on any other failure it returns a wrapped
// analysis.ErrUpstream with the body already closed.
func (c *Client) StreamChat(ctx context.Context, systemPrompt, userPrompt string) (io.ReadCloser, error) {
	payload, err := json.Marshal(
		chatRequest{
			Model:     model,
			MaxTokens: maxTokens,
			Stream:    true,
			Messages: []chatMessage{
				{Role: "system", Content: systemPrompt},
				{Role: "user", Content: userPrompt},
			},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("groq marshal request: %w", analysis.ErrUpstream)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("groq build request: %w", analysis.ErrUpstream)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("groq request failed: %w", analysis.ErrUpstream)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		resp.Body.Close()
		return nil, analysis.ErrRateLimited
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		return nil, fmt.Errorf("groq status %d: %s: %w", resp.StatusCode, snippet, analysis.ErrUpstream)
	}

	return resp.Body, nil
}
