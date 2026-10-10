// Package marketuc builds the index heatmaps: a snapshot per index, refreshed by
// a background job and served from memory.
package marketuc

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	domain "github.com/kanitin/stackvest/backend/internal/domain/market"
	stockdomain "github.com/kanitin/stackvest/backend/internal/domain/stock"
)

// changeConcurrency bounds in-flight price-change lookups per build. The
// provider budget is enforced by the throttled PriceChanger wired in main.go;
// this only caps goroutines waiting on it.
const changeConcurrency = 8

// otherSector groups constituents the provider returns without a sector.
const otherSector = "Other"

type HeatmapUseCase struct {
	lister  domain.ConstituentLister
	quoter  domain.BatchQuoter
	changer stockdomain.PriceChanger
	store   domain.SnapshotStore // optional; nil keeps snapshots in memory only
	now     func() time.Time

	mu        sync.RWMutex
	snapshots map[domain.Index]*domain.Heatmap
}

func NewHeatmapUseCase(lister domain.ConstituentLister, quoter domain.BatchQuoter, changer stockdomain.PriceChanger) *HeatmapUseCase {
	return &HeatmapUseCase{
		lister:    lister,
		quoter:    quoter,
		changer:   changer,
		now:       time.Now,
		snapshots: make(map[domain.Index]*domain.Heatmap),
	}
}

// WithStore persists every built snapshot to store, and lets Restore load them
// back after a restart.
func (uc *HeatmapUseCase) WithStore(store domain.SnapshotStore) *HeatmapUseCase {
	uc.store = store
	return uc
}

// Restore loads the stored snapshot of every index that has none in memory
// yet, so a restarted server can answer before its first rebuild finishes. It
// returns how many were restored; an index with nothing stored is not an error.
func (uc *HeatmapUseCase) Restore(ctx context.Context) (int, error) {
	if uc.store == nil {
		return 0, nil
	}
	restored := 0
	var errs []error
	for _, idx := range domain.Indexes {
		hm, err := uc.store.Load(ctx, idx)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", idx, err))
			continue
		}
		if hm == nil {
			continue
		}
		uc.mu.Lock()
		if _, ok := uc.snapshots[idx]; !ok {
			uc.snapshots[idx] = hm
			restored++
		}
		uc.mu.Unlock()
	}
	return restored, errors.Join(errs...)
}

// Get returns the latest snapshot of index, or ErrHeatmapNotReady before the
// first refresh of that index has finished.
func (uc *HeatmapUseCase) Get(index domain.Index) (*domain.Heatmap, error) {
	uc.mu.RLock()
	defer uc.mu.RUnlock()
	hm, ok := uc.snapshots[index]
	if !ok {
		return nil, domain.ErrHeatmapNotReady
	}
	return hm, nil
}

// Refresh rebuilds every index in turn, publishing each as soon as it is built.
// A failed index keeps its previous snapshot; the failures are returned joined.
func (uc *HeatmapUseCase) Refresh(ctx context.Context) error {
	var errs []error
	for _, idx := range domain.Indexes {
		if err := ctx.Err(); err != nil {
			return err
		}
		hm, err := uc.build(ctx, idx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			errs = append(errs, fmt.Errorf("%s: %w", idx, err))
			continue
		}
		uc.mu.Lock()
		uc.snapshots[idx] = hm
		uc.mu.Unlock()
		if uc.store != nil {
			// The snapshot is already served from memory; a failed save only
			// costs the fast restart, so it is reported but not fatal.
			if err := uc.store.Save(ctx, hm); err != nil {
				errs = append(errs, fmt.Errorf("%s: save snapshot: %w", idx, err))
			}
		}
	}
	return errors.Join(errs...)
}

func (uc *HeatmapUseCase) build(ctx context.Context, index domain.Index) (*domain.Heatmap, error) {
	constituents, err := uc.lister.ListConstituents(index)
	if err != nil {
		return nil, fmt.Errorf("list constituents: %w", err)
	}
	symbols := make([]string, len(constituents))
	for i, c := range constituents {
		symbols[i] = c.Symbol
	}

	quoteList, err := uc.quoter.GetBatchQuotes(symbols)
	if err != nil {
		return nil, fmt.Errorf("quotes: %w", err)
	}
	quotes := make(map[string]domain.Quote, len(quoteList))
	for _, q := range quoteList {
		quotes[q.Symbol] = q
	}

	changes, err := uc.priceChanges(ctx, symbols)
	if err != nil {
		return nil, err
	}

	bySector := make(map[string]*domain.Sector)
	for _, c := range constituents {
		q, ok := quotes[c.Symbol]
		if !ok || q.MarketCap <= 0 {
			continue // without a market cap the tile has no size
		}
		name := c.Sector
		if name == "" {
			name = otherSector
		}
		sec, ok := bySector[name]
		if !ok {
			sec = &domain.Sector{Name: name}
			bySector[name] = sec
		}
		d1 := q.ChangePercent
		change := domain.Change{D1: &d1}
		if pc := changes[c.Symbol]; pc != nil {
			// Copies, so the snapshot never aliases a cached PriceChange.
			w1, m1, ytd := pc.D5, pc.M1, pc.YTD
			change.W1, change.M1, change.YTD = &w1, &m1, &ytd
		}
		sec.Stocks = append(sec.Stocks, domain.Stock{
			Symbol:    c.Symbol,
			Name:      c.Name,
			SubSector: c.SubSector,
			MarketCap: q.MarketCap,
			Price:     q.Price,
			Change:    change,
		})
		sec.MarketCap += q.MarketCap
	}

	sectors := make([]domain.Sector, 0, len(bySector))
	for _, sec := range bySector {
		slices.SortFunc(sec.Stocks, func(a, b domain.Stock) int { return cmp.Compare(b.MarketCap, a.MarketCap) })
		sectors = append(sectors, *sec)
	}
	slices.SortFunc(sectors, func(a, b domain.Sector) int { return cmp.Compare(b.MarketCap, a.MarketCap) })

	return &domain.Heatmap{Index: index, UpdatedAt: uc.now().UTC(), Sectors: sectors}, nil
}

// priceChanges looks up the multi-period changes for each symbol. A symbol whose
// lookup fails is left out (its tile shows only the 1D change) rather than
// failing the whole map.
func (uc *HeatmapUseCase) priceChanges(ctx context.Context, symbols []string) (map[string]*stockdomain.PriceChange, error) {
	results := make([]*stockdomain.PriceChange, len(symbols))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(changeConcurrency)
	for i, sym := range symbols {
		if gctx.Err() != nil {
			break
		}
		g.Go(func() error {
			if gctx.Err() != nil {
				return gctx.Err()
			}
			if pc, err := uc.changer.GetPriceChange(sym); err == nil {
				results[i] = pc
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make(map[string]*stockdomain.PriceChange, len(symbols))
	for i, pc := range results {
		if pc != nil {
			out[symbols[i]] = pc
		}
	}
	return out, nil
}
