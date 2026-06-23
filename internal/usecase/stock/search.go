package stockuc

import (
	"log/slog"
	"strings"
	"sync"
	"time"

	domain "github.com/kanitin/stackvest/backend/internal/domain/stock"
	"github.com/kanitin/stackvest/backend/pkg/cache"
)

type SearchUseCase struct {
	searcher domain.Searcher
	lister   domain.SymbolLister
	universe *cache.TTL[*symbolUniverse]
}

func NewSearchUseCase(s domain.Searcher, lister domain.SymbolLister, universeTTL time.Duration) *SearchUseCase {
	return &SearchUseCase{
		searcher: s,
		lister:   lister,
		universe: cache.NewTTL[*symbolUniverse](universeTTL),
	}
}

// symbolUniverse holds the set of valid stock and ETF symbols (kept separate so
// the stock-vs-ETF distinction isn't lost), used to filter search results down
// to tradable stocks/ETFs. Symbols are stored uppercase for case-insensitive
// matching against search results.
type symbolUniverse struct {
	stocks map[string]struct{}
	etfs   map[string]struct{}
}

func (u *symbolUniverse) contains(symbol string) bool {
	key := strings.ToUpper(symbol)
	if _, ok := u.stocks[key]; ok {
		return true
	}
	_, ok := u.etfs[key]
	return ok
}

// Execute searches FMP by ticker and by company/brand name concurrently, merges
// the two result sets deduped by symbol (symbol matches first), then filters them
// down to symbols that are actual stocks or ETFs.
//
// Errors degrade gracefully: if one search endpoint fails but the other succeeds,
// the successful results are returned; an error is returned only when both fail.
// The stock/ETF filter is fail-safe — if the symbol universe is unavailable or
// implausibly empty, results are returned unfiltered rather than dropped.
func (uc *SearchUseCase) Execute(keywords string) ([]domain.Match, error) {
	if keywords == "" {
		return []domain.Match{}, nil
	}

	var (
		wg                 sync.WaitGroup
		symbolMatches      []domain.Match
		nameMatches        []domain.Match
		symbolErr, nameErr error
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		symbolMatches, symbolErr = uc.searcher.SearchSymbol(keywords)
	}()
	go func() {
		defer wg.Done()
		nameMatches, nameErr = uc.searcher.SearchName(keywords)
	}()
	wg.Wait()

	// Only fail when both endpoints fail; otherwise return whatever succeeded.
	if symbolErr != nil && nameErr != nil {
		return nil, symbolErr
	}

	seen := make(map[string]struct{}, len(symbolMatches)+len(nameMatches))
	merged := make([]domain.Match, 0, len(symbolMatches)+len(nameMatches))
	for _, group := range [][]domain.Match{symbolMatches, nameMatches} {
		for _, m := range group {
			if _, ok := seen[m.Symbol]; ok {
				continue
			}
			seen[m.Symbol] = struct{}{}
			merged = append(merged, m)
		}
	}

	return uc.filterToStocksAndETFs(keywords, merged), nil
}

// filterToStocksAndETFs keeps only matches whose symbol is a known stock or ETF.
// If the symbol universe is unavailable it fails safe by returning matches
// unchanged (better to over-return than to silently drop every result).
func (uc *SearchUseCase) filterToStocksAndETFs(keywords string, matches []domain.Match) []domain.Match {
	u := uc.loadUniverse()
	if u == nil {
		return matches
	}

	filtered := make([]domain.Match, 0, len(matches))
	for _, m := range matches {
		if u.contains(m.Symbol) {
			filtered = append(filtered, m)
		}
	}

	if dropped := len(matches) - len(filtered); dropped > 0 {
		slog.Debug("search: filtered non-stock/etf results",
			"keywords", keywords, "dropped", dropped, "kept", len(filtered))
	}
	return filtered
}

// loadUniverse returns the cached stock/ETF symbol universe, lazily fetching both
// lists on a cache miss. It returns nil (meaning "do not filter") whenever the
// universe can't be trusted: either list errors, or either list is empty — the
// latter guards against a deny-listed key or wrong path returning an empty 200,
// which would otherwise wipe out every search result.
func (uc *SearchUseCase) loadUniverse() *symbolUniverse {
	if u, ok := uc.universe.Get(); ok {
		return u
	}

	var (
		wg                 sync.WaitGroup
		stockSyms, etfSyms []string
		stockErr, etfErr   error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		stockSyms, stockErr = uc.lister.ListStockSymbols()
	}()
	go func() {
		defer wg.Done()
		etfSyms, etfErr = uc.lister.ListETFSymbols()
	}()
	wg.Wait()

	if stockErr != nil || etfErr != nil || len(stockSyms) == 0 || len(etfSyms) == 0 {
		slog.Warn("search: stock/ETF universe unavailable, returning unfiltered results",
			"stockErr", stockErr, "etfErr", etfErr,
			"stockCount", len(stockSyms), "etfCount", len(etfSyms))
		return nil
	}

	u := &symbolUniverse{
		stocks: toUpperSet(stockSyms),
		etfs:   toUpperSet(etfSyms),
	}
	uc.universe.Set(u)
	return u
}

func toUpperSet(symbols []string) map[string]struct{} {
	set := make(map[string]struct{}, len(symbols))
	for _, s := range symbols {
		set[strings.ToUpper(s)] = struct{}{}
	}
	return set
}
