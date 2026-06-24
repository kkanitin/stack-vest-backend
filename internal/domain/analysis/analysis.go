package analysis

import (
	"context"
	"errors"
	"io"
)

// ErrRateLimited is returned when the upstream provider responds 429.
var ErrRateLimited = errors.New("analysis: rate limited")

// ErrUpstream is returned for any other upstream failure (bad status, network error).
var ErrUpstream = errors.New("analysis: upstream failure")

// Streamer streams a chat completion as a raw Server-Sent Events byte stream.
type Streamer interface {
	// StreamChat POSTs the system and user prompts and returns the response body
	// for streaming.
	//
	// It inspects the HTTP status before returning: on 429 it returns
	// ErrRateLimited, on any other non-2xx status or transport error it returns a
	// wrapped ErrUpstream with the body already consumed and closed. On success it
	// returns the open body and the caller is responsible for closing it.
	StreamChat(ctx context.Context, systemPrompt, userPrompt string) (io.ReadCloser, error)
}
