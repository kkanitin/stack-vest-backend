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
	"sync/atomic"
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

// defaultProgressEvery is how often a running build reports progress, so a slow
// (rate-limited) build is visibly moving rather than looking stuck.
const defaultProgressEvery = 30 * time.Second

type EventKind int

const (
	// EventStarted: a build of Index began; Symbols is its constituent count.
	EventStarted EventKind = iota
	// EventProgress: a build is still running; Done of Symbols multi-period
	// lookups have finished.
	EventProgress
	// EventBuilt: Index was built and published with Stocks tiles. MissingChanges
	// symbols got no 1W/1M/YTD change; FirstChangeErr is one of those errors.
	EventBuilt
	// EventFailed: the build of Index failed with Err. ServingPrevious says
	// whether an older snapshot is still being served.
	EventFailed
	// EventSaveFailed: a built snapshot could not be persisted (Err).
	EventSaveFailed
)

// Event reports what a refresh is doing. Fields not listed for a kind are zero.
type Event struct {
	Kind            EventKind
	Index           domain.Index
	Symbols         int
	Done            int
	Stocks          int
	MissingChanges  int
	FirstChangeErr  error
	Elapsed         time.Duration
	ServingPrevious bool
	Err             error
}

// indexState tracks the latest build attempt of an index, so Get can tell
// "still building" apart from "the build failed".
type indexState struct {
	building bool
	lastErr  error // error of the last finished attempt; nil after a success
}

type HeatmapUseCase struct {
	lister  domain.ConstituentLister
	quoter  domain.BatchQuoter
	changer stockdomain.PriceChanger
	store   domain.SnapshotStore // optional; nil keeps snapshots in memory only
	onEvent func(Event)          // optional; nil discards events
	now     func() time.Time

	progressEvery time.Duration

	mu        sync.RWMutex
	snapshots map[domain.Index]*domain.Heatmap
	states    map[domain.Index]*indexState
}

func NewHeatmapUseCase(lister domain.ConstituentLister, quoter domain.BatchQuoter, changer stockdomain.PriceChanger) *HeatmapUseCase {
	return &HeatmapUseCase{
		lister:        lister,
		quoter:        quoter,
		changer:       changer,
		now:           time.Now,
		progressEvery: defaultProgressEvery,
		snapshots:     make(map[domain.Index]*domain.Heatmap),
		states:        make(map[domain.Index]*indexState),
	}
}

// WithEvents reports build progress and outcomes to fn. It is called from the
// refresh goroutine (and a progress ticker), so fn must be safe for concurrent use.
func (uc *HeatmapUseCase) WithEvents(fn func(Event)) *HeatmapUseCase {
	uc.onEvent = fn
	return uc
}

func (uc *HeatmapUseCase) emit(e Event) {
	if uc.onEvent != nil {
		uc.onEvent(e)
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

// Get returns the latest snapshot of index. Without one it returns
// ErrHeatmapNotReady while a build is running or queued, and
// ErrHeatmapUnavailable (wrapping the cause) when the last build failed.
func (uc *HeatmapUseCase) Get(index domain.Index) (*domain.Heatmap, error) {
	uc.mu.RLock()
	defer uc.mu.RUnlock()
	if hm, ok := uc.snapshots[index]; ok {
		return hm, nil
	}
	if st := uc.states[index]; st != nil && !st.building && st.lastErr != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrHeatmapUnavailable, st.lastErr)
	}
	return nil, domain.ErrHeatmapNotReady
}

func (uc *HeatmapUseCase) setState(index domain.Index, building bool, lastErr error) (servingPrevious bool) {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	st := uc.states[index]
	if st == nil {
		st = &indexState{}
		uc.states[index] = st
	}
	st.building = building
	if !building {
		st.lastErr = lastErr
	}
	_, servingPrevious = uc.snapshots[index]
	return servingPrevious
}

// Refresh rebuilds every index in turn, publishing each as soon as it is built.
// A failed index keeps its previous snapshot; the failures are returned joined.
func (uc *HeatmapUseCase) Refresh(ctx context.Context) error {
	var errs []error
	for _, idx := range domain.Indexes {
		if err := ctx.Err(); err != nil {
			return err
		}
		start := uc.now()
		uc.setState(idx, true, nil)
		hm, stats, err := uc.build(ctx, idx, start)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				uc.setState(idx, false, nil) // shutting down, not a failure
				return err
			}
			serving := uc.setState(idx, false, err)
			uc.emit(Event{Kind: EventFailed, Index: idx, Elapsed: uc.now().Sub(start), ServingPrevious: serving, Err: err})
			errs = append(errs, fmt.Errorf("%s: %w", idx, err))
			continue
		}
		uc.mu.Lock()
		uc.snapshots[idx] = hm
		uc.mu.Unlock()
		uc.setState(idx, false, nil)
		uc.emit(Event{
			Kind: EventBuilt, Index: idx, Symbols: stats.symbols, Stocks: stats.stocks,
			MissingChanges: stats.missingChanges, FirstChangeErr: stats.firstChangeErr, Elapsed: uc.now().Sub(start),
		})
		if uc.store != nil {
			// The snapshot is already served from memory; a failed save only
			// costs the fast restart, so it is reported but not fatal.
			if err := uc.store.Save(ctx, hm); err != nil {
				uc.emit(Event{Kind: EventSaveFailed, Index: idx, Err: err})
				errs = append(errs, fmt.Errorf("%s: save snapshot: %w", idx, err))
			}
		}
	}
	return errors.Join(errs...)
}

// buildStats summarises a successful build for its EventBuilt.
type buildStats struct {
	symbols        int
	stocks         int
	missingChanges int
	firstChangeErr error
}

func (uc *HeatmapUseCase) build(ctx context.Context, index domain.Index, start time.Time) (*domain.Heatmap, buildStats, error) {
	var stats buildStats
	constituents, err := uc.lister.ListConstituents(index)
	if err != nil {
		return nil, stats, fmt.Errorf("list constituents: %w", err)
	}
	symbols := make([]string, len(constituents))
	for i, c := range constituents {
		symbols[i] = c.Symbol
	}
	stats.symbols = len(symbols)
	uc.emit(Event{Kind: EventStarted, Index: index, Symbols: len(symbols)})

	quoteList, err := uc.quoter.GetBatchQuotes(symbols)
	if err != nil {
		return nil, stats, fmt.Errorf("quotes: %w", err)
	}
	quotes := make(map[string]domain.Quote, len(quoteList))
	for _, q := range quoteList {
		quotes[q.Symbol] = q
	}

	changes, firstChangeErr, err := uc.priceChanges(ctx, index, start, symbols)
	if err != nil {
		return nil, stats, err
	}
	stats.missingChanges = len(symbols) - len(changes)
	stats.firstChangeErr = firstChangeErr

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
		stats.stocks++
	}

	sectors := make([]domain.Sector, 0, len(bySector))
	for _, sec := range bySector {
		slices.SortFunc(sec.Stocks, func(a, b domain.Stock) int { return cmp.Compare(b.MarketCap, a.MarketCap) })
		sectors = append(sectors, *sec)
	}
	slices.SortFunc(sectors, func(a, b domain.Sector) int { return cmp.Compare(b.MarketCap, a.MarketCap) })

	return &domain.Heatmap{Index: index, UpdatedAt: uc.now().UTC(), Sectors: sectors}, stats, nil
}

// priceChanges looks up the multi-period changes for each symbol. A symbol whose
// lookup fails is left out (its tile shows only the 1D change) rather than
// failing the whole map; the first such error is returned for logging. While
// it runs it emits EventProgress every progressEvery.
func (uc *HeatmapUseCase) priceChanges(ctx context.Context, index domain.Index, start time.Time, symbols []string) (changes map[string]*stockdomain.PriceChange, sampleErr, err error) {
	results := make([]*stockdomain.PriceChange, len(symbols))
	var done atomic.Int64
	var firstErr atomic.Pointer[error]

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		t := time.NewTicker(uc.progressEvery)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				uc.emit(Event{Kind: EventProgress, Index: index, Symbols: len(symbols), Done: int(done.Load()), Elapsed: uc.now().Sub(start)})
			}
		}
	}()

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
			pc, err := uc.changer.GetPriceChange(sym)
			if err != nil {
				err = fmt.Errorf("%s: %w", sym, err)
				firstErr.CompareAndSwap(nil, &err)
			} else {
				results[i] = pc
			}
			done.Add(1)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	out := make(map[string]*stockdomain.PriceChange, len(symbols))
	for i, pc := range results {
		if pc != nil {
			out[symbols[i]] = pc
		}
	}
	var sample error
	if p := firstErr.Load(); p != nil {
		sample = *p
	}
	return out, sample, nil
}
