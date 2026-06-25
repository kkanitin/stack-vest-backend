package portfolio

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"

	"golang.org/x/sync/errgroup"

	portfoliodomain "github.com/kanitin/stackvest/backend/internal/domain/portfolio"
	stockdomain "github.com/kanitin/stackvest/backend/internal/domain/stock"
	userdomain "github.com/kanitin/stackvest/backend/internal/domain/user"
)

type UseCase struct {
	repo          portfoliodomain.Repository
	userRepo      userdomain.Repository
	quoter        stockdomain.Quoter
	priceChanger  stockdomain.PriceChanger
	maxPortfolios int
	maxPositions  int
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
	count, err := uc.repo.CountPortfolios(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	if count >= uc.maxPortfolios {
		return nil, portfoliodomain.ErrPortfolioLimitReached
	}
	return uc.repo.CreatePortfolio(ctx, user.ID, name, description)
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
	positions, err := uc.repo.ListByPortfolioID(ctx, portfolioID)
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

func (uc *UseCase) AddPosition(ctx context.Context, email, portfolioID, symbol, name string, shares, avgCost float64) (*portfoliodomain.Position, error) {
	if _, err := uc.ownedPortfolio(ctx, email, portfolioID); err != nil {
		return nil, err
	}
	count, err := uc.repo.CountPositions(ctx, portfolioID)
	if err != nil {
		return nil, err
	}
	if count >= uc.maxPositions {
		return nil, portfoliodomain.ErrPositionLimitReached
	}
	return uc.repo.Add(ctx, portfolioID, symbol, name, shares, avgCost)
}

func (uc *UseCase) RemovePosition(ctx context.Context, email, portfolioID, symbol string) error {
	if _, err := uc.ownedPortfolio(ctx, email, portfolioID); err != nil {
		return err
	}
	return uc.repo.Remove(ctx, portfolioID, symbol)
}

func (uc *UseCase) UpdatePosition(ctx context.Context, email, portfolioID, symbol string, shares, avgCost *float64) (*portfoliodomain.Position, error) {
	if _, err := uc.ownedPortfolio(ctx, email, portfolioID); err != nil {
		return nil, err
	}
	return uc.repo.Update(ctx, portfolioID, symbol, shares, avgCost)
}

func (uc *UseCase) ListPositions(ctx context.Context, email, portfolioID string) ([]*portfoliodomain.Position, error) {
	if _, err := uc.ownedPortfolio(ctx, email, portfolioID); err != nil {
		return nil, err
	}
	positions, err := uc.repo.ListByPortfolioID(ctx, portfolioID)
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
	positions, err := uc.repo.ListByPortfolioID(ctx, portfolioID)
	if err != nil {
		return nil, err
	}
	if len(positions) == 0 {
		return &portfoliodomain.Summary{}, nil
	}

	prices := uc.fetchPrices(ctx, distinctSymbols(positions))
	totalValue, totalValue30dAgo := aggregateValue(positions, prices)

	change30d := totalValue - totalValue30dAgo
	var changePct30d float64
	if totalValue30dAgo != 0 {
		changePct30d = change30d / totalValue30dAgo * 100
	}
	return &portfoliodomain.Summary{
		TotalValue:   totalValue,
		Change30d:    change30d,
		ChangePct30d: changePct30d,
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
	if len(positions) == 0 {
		return &portfoliodomain.PortfoliosSummary{}, nil
	}

	prices := uc.fetchPrices(ctx, distinctSymbols(positions))
	totalValue, totalValue30dAgo := aggregateValue(positions, prices)

	var changePct float64
	if totalValue30dAgo != 0 {
		changePct = (totalValue - totalValue30dAgo) / totalValue30dAgo * 100
	}

	// Concentration is measured per symbol (exposure to a ticker held in several
	// portfolios is combined), not per holding line.
	valueBySymbol := make(map[string]float64)
	for _, pos := range positions {
		pd, ok := prices[pos.Symbol]
		if !ok {
			continue
		}
		valueBySymbol[pos.Symbol] += pos.Shares * pd.currentPrice
	}

	return &portfoliodomain.PortfoliosSummary{
		TotalValue:           totalValue,
		ChangePct:            changePct,
		DiversificationScore: diversificationScore(valueBySymbol),
	}, nil
}

func (uc *UseCase) GetActivity(ctx context.Context, email, portfolioID string, limit int) ([]*portfoliodomain.Activity, error) {
	if _, err := uc.ownedPortfolio(ctx, email, portfolioID); err != nil {
		return nil, err
	}
	return uc.repo.GetActivity(ctx, portfolioID, limit)
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
	positions, err := uc.repo.ListByPortfolioID(ctx, portfolioID)
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

// fetchPrices concurrently fetches the quote and 30-day change for each distinct
// symbol. Lookups are best-effort: a symbol whose quote or change fails is omitted
// from the returned map (callers treat a missing entry as "no value available").
func (uc *UseCase) fetchPrices(ctx context.Context, symbols []string) map[string]priceData {
	out := make(map[string]priceData, len(symbols))
	var mu sync.Mutex
	g := new(errgroup.Group)
	for _, sym := range symbols {
		sym := sym
		g.Go(func() error {
			q, err := uc.quoter.GetQuote(sym)
			if err != nil {
				slog.WarnContext(ctx, "failed to get quote", "symbol", sym, "error", err)
				return nil
			}
			pc, err := uc.priceChanger.GetPriceChange(sym)
			if err != nil {
				slog.WarnContext(ctx, "failed to get price change", "symbol", sym, "error", err)
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

func (uc *UseCase) enrichPositions(ctx context.Context, positions []*portfoliodomain.Position) {
	g := new(errgroup.Group)
	for _, pos := range positions {
		pos := pos
		g.Go(func() error {
			q, err := uc.quoter.GetQuote(pos.Symbol)
			if err != nil {
				slog.WarnContext(ctx, "failed to get quote", "symbol", pos.Symbol, "error", err)
				return nil
			}
			pos.ValueUsd = pos.Shares * q.Price
			pos.Change24h = q.ChangePercent
			return nil
		})
	}
	g.Wait()
}
