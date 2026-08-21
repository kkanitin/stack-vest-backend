package groq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/kanitin/stackvest/backend/internal/domain/analysis"
)

// maxCompletionTokens bounds the generated response. Every model in the chain below
// is a reasoning model, and reasoning tokens are charged against this same budget —
// so this has to leave room for both the hidden reasoning pass and the JSON payload
// the analysis prompt asks for. Sized well above the ~400 tokens the JSON itself
// needs; too tight a budget yields truncated JSON rather than an error.
const maxCompletionTokens = 1600

// reasoningFormat keeps reasoning tokens out of the response entirely. This is not
// optional: StreamChat forwards the raw SSE stream to the browser and the analysis
// system prompt demands a bare JSON object, so reasoning deltas interleaved into the
// content would corrupt the payload the frontend parses.
const reasoningFormat = "hidden"

// modelConfig is one entry in the fallback chain. reasoningEffort is per-model
// because the accepted values differ by family: the gpt-oss models take
// low/medium/high, while qwen3 takes none/default.
type modelConfig struct {
	id              string
	reasoningEffort string
}

// models is the fallback chain, tried in order until one returns a 2xx. Groq
// decommissioned llama-3.3-70b-versatile for free and developer tiers in 2026,
// which is why this is a chain rather than a single constant — a model vanishing
// upstream should degrade the analysis, not break the endpoint.
var models = []modelConfig{
	{id: "openai/gpt-oss-120b", reasoningEffort: "low"},
	{id: "openai/gpt-oss-20b", reasoningEffort: "low"},
	{id: "qwen/qwen3.6-27b", reasoningEffort: "none"},
}

// Attempt outcomes that decide whether the chain continues.
//
// errAbort stops it. Two things earn that: a credential rejection, since every model
// shares one API key and would repeat the same rejection; and a local failure to
// build the request, which is deterministic and has nothing to do with the model.
// Retrying either would just burn a round trip per remaining model.
var (
	errAbort     = errors.New("groq: unrecoverable, chain aborted")
	errRateLimit = errors.New("groq: rate limited")
	errRetry     = errors.New("groq: attempt failed, next model may work")
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
		//
		// ResponseHeaderTimeout now bounds each link in the fallback chain rather
		// than the whole call, so the worst case before an error surfaces is
		// len(models) × 20s. The per-iteration ctx check in StreamChat is what keeps
		// an abandoned request from paying that full cost.
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
	Model string `json:"model"`
	// max_completion_tokens, not max_tokens: Groq deprecated the latter, and on
	// reasoning models it is the completion budget that reasoning draws from.
	MaxCompletionTokens int           `json:"max_completion_tokens"`
	ReasoningEffort     string        `json:"reasoning_effort,omitempty"`
	ReasoningFormat     string        `json:"reasoning_format,omitempty"`
	Stream              bool          `json:"stream"`
	Messages            []chatMessage `json:"messages"`
}

// StreamChat implements analysis.Streamer. It POSTs the prompt with stream=true,
// walking the model fallback chain until one responds 2xx, and returns that model's
// raw SSE response body for the caller to forward.
//
// The chain is walked entirely before any bytes are handed back, so a fallback is
// never visible to the caller as a partial stream. It stops early on a credential
// failure. If every model responds 429 it returns analysis.ErrRateLimited; on any
// other exhaustion it returns a wrapped analysis.ErrUpstream naming every attempt,
// with all bodies already closed.
func (c *Client) StreamChat(ctx context.Context, systemPrompt, userPrompt string) (io.ReadCloser, error) {
	messages := []chatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}

	notes := make([]string, 0, len(models))
	rateLimited := 0

	for i, m := range models {
		// Checked per iteration rather than relying on the transport: once the
		// caller is gone, trying the remaining models wastes a full
		// ResponseHeaderTimeout each. Wrapping both errors keeps errors.Is working
		// for context.Canceled and for the ErrUpstream the handler maps to 502.
		if ctx.Err() != nil {
			return nil, fmt.Errorf("groq canceled after %d attempt(s) [%s]: %w: %w",
				len(notes), strings.Join(notes, "; "), ctx.Err(), analysis.ErrUpstream)
		}

		body, note, err := c.streamOne(ctx, m, messages)
		if err == nil {
			if i > 0 {
				zap.L().Warn("groq serving analysis from fallback model",
					zap.String("model", m.id),
					zap.Int("position", i+1),
					zap.String("failed_attempts", strings.Join(notes, "; ")),
				)
			}
			return body, nil
		}

		notes = append(notes, note)

		if errors.Is(err, errAbort) {
			zap.L().Error("groq fallback chain aborted, remaining models skipped", zap.String("attempt", note))
			return nil, fmt.Errorf("groq chain aborted [%s]: %w", note, analysis.ErrUpstream)
		}

		zap.L().Warn("groq model attempt failed, trying next in chain",
			zap.String("attempt", note),
			zap.Int("position", i+1),
			zap.Int("of", len(models)),
		)

		if errors.Is(err, errRateLimit) {
			rateLimited++
		}
	}

	// Only a chain that was rate-limited end to end is worth reporting as such:
	// that is the one case where the caller retrying later actually helps.
	if rateLimited == len(models) {
		return nil, analysis.ErrRateLimited
	}
	return nil, fmt.Errorf("groq exhausted %d model(s) [%s]: %w",
		len(models), strings.Join(notes, "; "), analysis.ErrUpstream)
}

// streamOne runs a single attempt against one model. On success it returns the open
// response body, which is the caller's to close. On failure it returns a nil body, a
// short note naming the model and what went wrong (collected into the aggregate
// error so a failed chain reports every link, not just the last), and one of
// errAbort, errRateLimit, or errRetry.
func (c *Client) streamOne(ctx context.Context, m modelConfig, messages []chatMessage) (io.ReadCloser, string, error) {
	payload, err := json.Marshal(
		chatRequest{
			Model:               m.id,
			MaxCompletionTokens: maxCompletionTokens,
			ReasoningEffort:     m.reasoningEffort,
			ReasoningFormat:     reasoningFormat,
			Stream:              true,
			Messages:            messages,
		},
	)
	if err != nil {
		return nil, fmt.Sprintf("model=%s marshal request: %v", m.id, err), errAbort
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Sprintf("model=%s build request: %v", m.id, err), errAbort
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Sprintf("model=%s transport: %v", m.id, err), errRetry
	}

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		resp.Body.Close()
		// Groq meters per model id, so the next model has its own headroom.
		return nil, fmt.Sprintf("model=%s status=429", m.id), errRateLimit

	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Sprintf("model=%s status=%d: %s", m.id, resp.StatusCode, drain(resp)), errAbort

	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return nil, fmt.Sprintf("model=%s status=%d: %s", m.id, resp.StatusCode, drain(resp)), errRetry
	}

	return resp.Body, "", nil
}

// drain reads a bounded snippet of an error response and closes the body.
func drain(resp *http.Response) string {
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	resp.Body.Close()
	return string(snippet)
}
