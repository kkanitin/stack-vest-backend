package stockuc

import (
	"context"
	"errors"
	"strings"

	domain "github.com/kanitin/stackvest/backend/internal/domain/stock"
	"golang.org/x/sync/errgroup"
)

const maxBatchSymbols = 10

type BatchPriceChangeUseCase struct {
	changer domain.PriceChanger
}

func NewBatchPriceChangeUseCase(c domain.PriceChanger) *BatchPriceChangeUseCase {
	return &BatchPriceChangeUseCase{changer: c}
}

func (uc *BatchPriceChangeUseCase) Execute(ctx context.Context, symbolsParam string) ([]*domain.PriceChange, error) {
	syms, err := parseSymbols(symbolsParam)
	if err != nil {
		return nil, err
	}

	g, _ := errgroup.WithContext(ctx)
	results := make([]*domain.PriceChange, len(syms))
	for i, sym := range syms {
		g.Go(func() error {
			pc, err := uc.changer.GetPriceChange(sym)
			if errors.Is(err, domain.ErrSymbolNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			results[i] = pc
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	out := make([]*domain.PriceChange, 0, len(results))
	for _, r := range results {
		if r != nil {
			out = append(out, r)
		}
	}
	return out, nil
}

// parseSymbols splits a comma-separated symbols query parameter, trims whitespace,
// upper-cases, deduplicates while preserving order, and enforces the max item count.
func parseSymbols(symbolsParam string) ([]string, error) {
	parts := strings.Split(symbolsParam, ",")
	seen := make(map[string]struct{}, len(parts))
	out := make([]string, 0, len(parts))
	for _, s := range parts {
		sym := strings.ToUpper(strings.TrimSpace(s))
		if sym == "" {
			continue
		}
		if _, ok := seen[sym]; ok {
			continue
		}
		seen[sym] = struct{}{}
		out = append(out, sym)
	}
	if len(out) > maxBatchSymbols {
		return nil, domain.ErrTooManySymbols
	}
	return out, nil
}
