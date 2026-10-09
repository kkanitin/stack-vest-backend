package portfolio

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	portfoliodomain "github.com/kanitin/stackvest/backend/internal/domain/portfolio"
	stockdomain "github.com/kanitin/stackvest/backend/internal/domain/stock"
	userdomain "github.com/kanitin/stackvest/backend/internal/domain/user"
	"github.com/kanitin/stackvest/backend/pkg/logger"
)

type UseCase struct {
	repo          portfoliodomain.Repository
	userRepo      userdomain.Repository
	quoter        stockdomain.Quoter
	priceChanger  stockdomain.PriceChanger
	maxPortfolios int
	maxPositions  int

	// Benchmark overlay for GetValueHistory; set by WithBenchmarks (see benchmark.go).
	benchmarkFetcher stockdomain.HistoryFetcher
	benchmarks       []portfoliodomain.Benchmark
}

func New(
	repo portfoliodomain.Repository,
	userRepo userdomain.Repository,
	quoter stockdomain.Quoter,
	priceChanger stockdomain.PriceChanger,
	maxPortfolios int,
	maxPositions int,
) *UseCase {
	return &UseCase{
		repo:          repo,
		userRepo:      userRepo,
		quoter:        quoter,
		priceChanger:  priceChanger,
		maxPortfolios: maxPortfolios,
		maxPositions:  maxPositions,
	}
}

// ownedPortfolio verifies that the portfolio identified by portfolioID belongs to
// the authenticated user. A portfolio that does not exist or is owned by someone
// else both surface as ErrPortfolioNotFound (404) so existence is not leaked.
func (uc *UseCase) ownedPortfolio(ctx context.Context, email, portfolioID string) (*portfoliodomain.Portfolio, error) {
	user, err := uc.userRepo.FindByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("user lookup: %w", err)
	}
	p, err := uc.repo.GetPortfolio(ctx, portfolioID)
	if err != nil {
		return nil, err
	}
	if p.UserID != user.ID {
		return nil, portfoliodomain.ErrPortfolioNotFound
	}
	return p, nil
}

// --- Portfolios ---

func (uc *UseCase) CreatePortfolio(ctx context.Context, email, name, description string) (*portfoliodomain.Portfolio, error) {
	user, err := uc.userRepo.FindByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("user lookup: %w", err)
	}
	return uc.repo.CreatePortfolio(ctx, user.ID, name, description, uc.maxPortfolios)
}

func (uc *UseCase) ListPortfolios(ctx context.Context, email string) ([]*portfoliodomain.Portfolio, error) {
	user, err := uc.userRepo.FindByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("user lookup: %w", err)
	}
	portfolios, err := uc.repo.ListPortfolios(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	positions, err := uc.repo.ListPositionsByUser(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	uc.enrichPortfolios(ctx, portfolios, positions)
	return portfolios, nil
}

func (uc *UseCase) GetPortfolio(ctx context.Context, email, portfolioID string) (*portfoliodomain.Portfolio, error) {
	p, err := uc.ownedPortfolio(ctx, email, portfolioID)
	if err != nil {
		return nil, err
	}
	positions, err := uc.repo.ListByPortfolioID(ctx, portfolioID, false)
	if err != nil {
		return nil, err
	}
	uc.enrichPortfolios(ctx, []*portfoliodomain.Portfolio{p}, positions)
	return p, nil
}

func (uc *UseCase) UpdatePortfolio(ctx context.Context, email, portfolioID string, name, description *string) (*portfoliodomain.Portfolio, error) {
	if _, err := uc.ownedPortfolio(ctx, email, portfolioID); err != nil {
		return nil, err
	}
	return uc.repo.UpdatePortfolio(ctx, portfolioID, name, description)
}

func (uc *UseCase) DeletePortfolio(ctx context.Context, email, portfolioID string) error {
	if _, err := uc.ownedPortfolio(ctx, email, portfolioID); err != nil {
		return err
	}
	return uc.repo.DeletePortfolio(ctx, portfolioID)
}

// --- Positions ---

// AddPosition is the legacy "add a holding" entry, kept so already-deployed clients keep
// working: it records a buy dated today (UTC) with no fee and returns the position. As
// before the ledger, adding a symbol that is already an open position is refused with
// ErrAlreadyExists; a closed holding may be reopened. (The check is not atomic with the
// insert; a racing duplicate just becomes another buy, which the ledger handles.)
func (uc *UseCase) AddPosition(ctx context.Context, email, portfolioID, symbol, name string, shares, avgCost float64) (*portfoliodomain.Position, error) {
	if _, err := uc.ownedPortfolio(ctx, email, portfolioID); err != nil {
		return nil, err
	}
	open, err := uc.repo.ListByPortfolioID(ctx, portfolioID, false)
	if err != nil {
		return nil, err
	}
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	for _, p := range open {
		if p.Symbol == sym && p.Shares > 0 {
			return nil, portfoliodomain.ErrAlreadyExists
		}
	}
	res, err := uc.CreateTransaction(ctx, email, portfolioID, TransactionInput{
		Symbol:   symbol,
		Name:     name,
		Side:     portfoliodomain.SideBuy,
		Quantity: shares,
		Price:    avgCost,
		Date:     time.Now().UTC().Format(portfoliodomain.DateLayout),
	})
	if err != nil {
		return nil, err
	}
	return res.Position, nil
}

func (uc *UseCase) RemovePosition(ctx context.Context, email, portfolioID, symbol string) error {
	if _, err := uc.ownedPortfolio(ctx, email, portfolioID); err != nil {
		return err
	}
	return uc.repo.Remove(ctx, portfolioID, symbol)
}

// UpdatePosition no longer edits anything: holdings change only through their
// transactions. After the ownership check it returns ErrHoldingEditGone (410).
func (uc *UseCase) UpdatePosition(ctx context.Context, email, portfolioID, symbol string) (*portfoliodomain.Position, error) {
	if _, err := uc.ownedPortfolio(ctx, email, portfolioID); err != nil {
		return nil, err
	}
	return nil, ErrHoldingEditGone
}

// ListPositions returns the portfolio's holdings with live value and P&L. Closed holdings
// (zero shares) are included only with includeClosed.
func (uc *UseCase) ListPositions(ctx context.Context, email, portfolioID string, includeClosed bool) ([]*portfoliodomain.Position, error) {
	if _, err := uc.ownedPortfolio(ctx, email, portfolioID); err != nil {
		return nil, err
	}
	positions, err := uc.repo.ListByPortfolioID(ctx, portfolioID, includeClosed)
	if err != nil {
		return nil, err
	}
	uc.enrichPositions(ctx, positions)
	return positions, nil
}

// ListAllPositions returns every position across all of the user's portfolios, each
// enriched with its live value. A symbol held in several portfolios appears once per
// portfolio; callers that want one row per symbol merge them.
func (uc *UseCase) ListAllPositions(ctx context.Context, email string) ([]*portfoliodomain.Position, error) {
	user, err := uc.userRepo.FindByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("user lookup: %w", err)
	}
	positions, err := uc.repo.ListPositionsByUser(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	uc.enrichPositions(ctx, positions)
	return positions, nil
}

func (uc *UseCase) GetSummary(ctx context.Context, email, portfolioID string) (*portfoliodomain.Summary, error) {
	if _, err := uc.ownedPortfolio(ctx, email, portfolioID); err != nil {
		return nil, err
	}
	positions, err := uc.repo.ListByPortfolioID(ctx, portfolioID, false)
	if err != nil {
		return nil, err
	}
	txs, err := uc.repo.ListTransactions(ctx, portfolioID, "")
	if err != nil {
		return nil, err
	}
	if len(positions) == 0 && len(txs) == 0 {
		return &portfoliodomain.Summary{}, nil
	}

	prices := uc.fetchQuotePrices(ctx, distinctSymbols(positions))
	totalValue := positionsValue(positions, prices)
	pct, gain := uc.windowReturn(ctx, txs, prices, time.Now())
	return &portfoliodomain.Summary{
		TotalValue:    totalValue,
		Change30d:     gain,
		ChangePct30d:  pct,
		RealisedPnl:   ledgerRealised(txs),
		UnrealisedPnl: unrealisedPnl(positions, prices),
	}, nil
}

// GetPortfoliosSummary aggregates value and 30-day change across all of the user's
// portfolios and derives a 0–100 diversification score from holding concentration.
func (uc *UseCase) GetPortfoliosSummary(ctx context.Context, email string) (*portfoliodomain.PortfoliosSummary, error) {
	user, err := uc.userRepo.FindByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("user lookup: %w", err)
	}
	positions, err := uc.repo.ListPositionsByUser(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	txs, err := uc.repo.ListTransactionsByUser(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	if len(positions) == 0 && len(txs) == 0 {
		return &portfoliodomain.PortfoliosSummary{}, nil
	}

	prices := uc.fetchQuotePrices(ctx, distinctSymbols(positions))
	totalValue := positionsValue(positions, prices)
	changePct, _ := uc.windowReturn(ctx, txs, prices, time.Now())

	// Concentration is measured per symbol (exposure to a ticker held in several
	// portfolios is combined), not per holding line.
	valueBySymbol := make(map[string]float64)
	for _, pos := range positions {
		price, ok := prices[pos.Symbol]
		if !ok {
			continue
		}
		valueBySymbol[pos.Symbol] += pos.Shares * price
	}

	return &portfoliodomain.PortfoliosSummary{
		TotalValue:           totalValue,
		ChangePct:            changePct,
		RealisedPnl:          ledgerRealised(txs),
		UnrealisedPnl:        unrealisedPnl(positions, prices),
		DiversificationScore: diversificationScore(valueBySymbol),
	}, nil
}

func (uc *UseCase) GetActivity(ctx context.Context, email, portfolioID string, limit int) ([]*portfoliodomain.Activity, error) {
	if _, err := uc.ownedPortfolio(ctx, email, portfolioID); err != nil {
		return nil, err
	}
	return uc.repo.GetActivity(ctx, portfolioID, limit)
}

// GetRecentActivity returns the newest activity across all of the user's portfolios.
func (uc *UseCase) GetRecentActivity(ctx context.Context, email string, limit int) ([]*portfoliodomain.Activity, error) {
	user, err := uc.userRepo.FindByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("user lookup: %w", err)
	}
	return uc.repo.GetActivityByUser(ctx, user.ID, limit)
}

// AnalysisHolding is a stored holding paired with its current market-value weight
// (percent of the portfolio's total priced value).
type AnalysisHolding struct {
	Ticker string
	Weight float64
}

// AnalysisData is the snapshot of a stored portfolio used to drive an AI analysis: its
// name/description and each holding's current market-value weight.
type AnalysisData struct {
	Name        string
	Description string
	Holdings    []AnalysisHolding
}

// BuildAnalysisData loads an owned portfolio and its holdings, prices them, and returns
// each holding's current market-value weight (percent). Weights are computed over the
// priced subset — unpriced symbols are dropped (consistent with the rest of the package)
// and the remaining weights sum to ~100. Returns ErrPortfolioNotFound (404) when the
// portfolio is missing or not owned, ErrPortfolioEmpty when it has no holdings, or
// ErrPricingUnavailable when it has holdings but none could be priced.
func (uc *UseCase) BuildAnalysisData(ctx context.Context, email, portfolioID string) (*AnalysisData, error) {
	p, err := uc.ownedPortfolio(ctx, email, portfolioID)
	if err != nil {
		return nil, err
	}
	positions, err := uc.repo.ListByPortfolioID(ctx, portfolioID, false)
	if err != nil {
		return nil, err
	}
	if len(positions) == 0 {
		return nil, portfoliodomain.ErrPortfolioEmpty
	}

	// enrichPositions sets ValueUsd from the live quote (best-effort, quote-only — it does
	// not depend on the 30-day price change, which analysis doesn't need). A symbol whose
	// quote fails keeps ValueUsd 0 and is dropped from the weight basis; the survivors
	// renormalize to ~100.
	uc.enrichPositions(ctx, positions)

	var total float64
	for _, pos := range positions {
		if pos.ValueUsd > 0 {
			total += pos.ValueUsd
		}
	}
	if total <= 0 {
		return nil, portfoliodomain.ErrPricingUnavailable
	}

	holdings := make([]AnalysisHolding, 0, len(positions))
	for _, pos := range positions {
		if pos.ValueUsd <= 0 {
			continue
		}
		holdings = append(holdings, AnalysisHolding{Ticker: pos.Symbol, Weight: pos.ValueUsd / total * 100})
	}
	return &AnalysisData{Name: p.Name, Description: p.Description, Holdings: holdings}, nil
}

// priceData holds the latest quote and 30-day price change for a symbol. ok is false
// when either lookup failed, signalling callers to exclude the symbol from value math.
type priceData struct {
	currentPrice float64
	change1M     float64
	ok           bool
}

// fetchConcurrency bounds simultaneous outbound FMP connections per request —
// without it, fan-out width equals distinct-symbol count (up to 200 at the
// configured MaxPerUser x MaxPositionsPerPortfolio ceiling).
const fetchConcurrency = 10

// fetchPrices concurrently fetches the quote and 30-day change for each distinct
// symbol (the two calls for a given symbol run in parallel with each other too,
// since neither depends on the other's result). Lookups are best-effort: a
// symbol whose quote or change fails is omitted from the returned map (callers
// treat a missing entry as "no value available").
func (uc *UseCase) fetchPrices(ctx context.Context, symbols []string) map[string]priceData {
	out := make(map[string]priceData, len(symbols))
	var mu sync.Mutex
	g := new(errgroup.Group)
	g.SetLimit(fetchConcurrency)
	for _, sym := range symbols {
		sym := sym
		g.Go(func() error {
			var q *stockdomain.Quote
			var pc *stockdomain.PriceChange
			var qErr, pcErr error
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				q, qErr = uc.quoter.GetQuote(sym)
			}()
			go func() {
				defer wg.Done()
				pc, pcErr = uc.priceChanger.GetPriceChange(sym)
			}()
			wg.Wait()
			if qErr != nil {
				zap.L().Warn("failed to get quote", logger.RequestID(ctx), zap.String("symbol", sym), zap.Error(qErr))
				return nil
			}
			if pcErr != nil {
				zap.L().Warn("failed to get price change", logger.RequestID(ctx), zap.String("symbol", sym), zap.Error(pcErr))
				return nil
			}
			mu.Lock()
			out[sym] = priceData{currentPrice: q.Price, change1M: pc.M1, ok: true}
			mu.Unlock()
			return nil
		})
	}
	g.Wait()
	return out
}

// distinctSymbols returns the unique symbols across positions so each ticker is
// quoted only once, even when held in multiple portfolios.
func distinctSymbols(positions []*portfoliodomain.Position) []string {
	seen := make(map[string]struct{}, len(positions))
	symbols := make([]string, 0, len(positions))
	for _, pos := range positions {
		if _, ok := seen[pos.Symbol]; ok {
			continue
		}
		seen[pos.Symbol] = struct{}{}
		symbols = append(symbols, pos.Symbol)
	}
	return symbols
}

// positionsValue sums the current USD value of the positions that have a price.
func positionsValue(positions []*portfoliodomain.Position, prices map[string]float64) float64 {
	var total float64
	for _, pos := range positions {
		if price, ok := prices[pos.Symbol]; ok {
			total += pos.Shares * price
		}
	}
	return total
}

// aggregateValue sums the current USD value of positions and their value 30 days
// ago (derived from each symbol's 1-month change). Positions whose price is
// unavailable, or whose 30-day-ago value is undefined, are skipped.
func aggregateValue(positions []*portfoliodomain.Position, prices map[string]priceData) (totalValue, totalValue30dAgo float64) {
	for _, pos := range positions {
		pd, ok := prices[pos.Symbol]
		if !ok {
			continue
		}
		value := pos.Shares * pd.currentPrice
		divisor := 1 + pd.change1M/100
		if divisor == 0 {
			continue
		}
		totalValue += value
		totalValue30dAgo += value / divisor
	}
	return totalValue, totalValue30dAgo
}

// diversificationScore maps holding-value concentration to a 0–100 score using the
// Herfindahl-Hirschman Index over per-symbol value weights: score = (1 − Σ wᵢ²)×100.
// A single holding scores 0; N equally-weighted holdings approach (1 − 1/N)×100.
func diversificationScore(valueBySymbol map[string]float64) int {
	var total float64
	for _, v := range valueBySymbol {
		total += v
	}
	if total <= 0 {
		return 0
	}
	var hhi float64
	for _, v := range valueBySymbol {
		w := v / total
		hhi += w * w
	}
	return int(math.Round((1 - hhi) * 100))
}

// enrichPortfolios populates each portfolio's derived Value and AssetCount from the
// supplied positions (which may span several portfolios). Symbols are quoted once.
// AssetCount comes straight from the holdings count (always known here). Value is left
// nil when a portfolio has holdings but none could be priced, so an upstream quote
// outage surfaces as null ("—") rather than a misleading $0.
func (uc *UseCase) enrichPortfolios(ctx context.Context, portfolios []*portfoliodomain.Portfolio, positions []*portfoliodomain.Position) {
	byPortfolio := make(map[string][]*portfoliodomain.Position, len(portfolios))
	for _, pos := range positions {
		if pos.Shares <= 0 {
			continue // closed holding: neither counted nor valued
		}
		byPortfolio[pos.PortfolioID] = append(byPortfolio[pos.PortfolioID], pos)
	}
	prices := uc.fetchPrices(ctx, distinctSymbols(positions))
	for _, p := range portfolios {
		group := byPortfolio[p.ID]
		count := len(group)
		p.AssetCount = &count
		if count > 0 && !anyPriced(group, prices) {
			p.Value = nil // holdings exist but pricing failed → leave null
			continue
		}
		value, _ := aggregateValue(group, prices)
		p.Value = &value
	}
}

// anyPriced reports whether at least one position in the group has a usable price.
func anyPriced(positions []*portfoliodomain.Position, prices map[string]priceData) bool {
	for _, pos := range positions {
		if _, ok := prices[pos.Symbol]; ok {
			return true
		}
	}
	return false
}

// enrichPositions sets each position's cost basis and closed flag, then (for open
// holdings, best-effort) its live value, 24h change and unrealised P&L. Closed holdings
// are not quoted. Without a quoter the live fields stay zero.
func (uc *UseCase) enrichPositions(ctx context.Context, positions []*portfoliodomain.Position) {
	g := new(errgroup.Group)
	g.SetLimit(fetchConcurrency)
	for _, pos := range positions {
		pos := pos
		pos.CostBasis = portfoliodomain.Round8(pos.Shares * pos.AvgCost)
		pos.Closed = pos.Shares <= 0
		if pos.Closed || uc.quoter == nil {
			continue
		}
		g.Go(func() error {
			q, err := uc.quoter.GetQuote(pos.Symbol)
			if err != nil {
				zap.L().Warn("failed to get quote", logger.RequestID(ctx), zap.String("symbol", pos.Symbol), zap.Error(err))
				return nil
			}
			pos.ValueUsd = pos.Shares * q.Price
			pos.Change24h = q.ChangePercent
			pos.UnrealisedPnl = pos.ValueUsd - pos.CostBasis
			if pos.CostBasis > 0 {
				pos.UnrealisedPnlPct = pos.UnrealisedPnl / pos.CostBasis * 100
			}
			return nil
		})
	}
	g.Wait()
}
