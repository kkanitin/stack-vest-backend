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

// fetchConcurrency bounds in-flight per-symbol lookups (profile + price change)
// per build. The provider budget is enforced by the throttled decorators wired
// in main.go; this only caps goroutines waiting on them.
const fetchConcurrency = 8

// otherSector groups constituents the provider returns without a sector.
const otherSector = "Other"

// defaultProgressEvery is how often a running build reports progress, so a slow
// (rate-limited) build is visibly moving rather than looking stuck.
const defaultProgressEvery = 30 * time.Second

type EventKind int

const (
	// EventStarted: a build of Index began; Symbols is its constituent count.
	EventStarted EventKind = iota
	// EventProgress: a build is still running; Done of Symbols have been
	// looked up (profile and price change).
	EventProgress
	// EventBuilt: Index was built and published with Stocks tiles.
	// MissingProfiles symbols were left out (no profile or market cap) and
	// MissingChanges tiles have no period changes; SampleErr is one such error.
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
	MissingProfiles int
	MissingChanges  int
	SampleErr       error
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
	lister   domain.ConstituentLister
	profiles stockdomain.ProfileFetcher
	changer  stockdomain.PriceChanger
	store    domain.SnapshotStore // optional; nil keeps snapshots in memory only
	onEvent  func(Event)          // optional; nil discards events
	now      func() time.Time

	progressEvery time.Duration

	mu        sync.RWMutex
	snapshots map[domain.Index]*domain.Heatmap
	states    map[domain.Index]*indexState
}

// NewHeatmapUseCase builds maps from index members (lister), each member's
// profile (name, sector, market cap, price) and its price changes.
func NewHeatmapUseCase(lister domain.ConstituentLister, profiles stockdomain.ProfileFetcher, changer stockdomain.PriceChanger) *HeatmapUseCase {
	return &HeatmapUseCase{
		lister:        lister,
		profiles:      profiles,
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
			MissingProfiles: stats.missingProfiles, MissingChanges: stats.missingChanges, SampleErr: stats.sampleErr,
			Elapsed: uc.now().Sub(start),
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
	symbols         int
	stocks          int
	missingProfiles int
	missingChanges  int
	sampleErr       error
}

// lookup is what one symbol's fetch produced; either part may be missing.
type lookup struct {
	profile *stockdomain.CompanyProfile
	change  *stockdomain.PriceChange
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

	lookups, sampleErr, err := uc.fetch(ctx, index, start, symbols)
	if err != nil {
		return nil, stats, err
	}
	stats.sampleErr = sampleErr

	bySector := make(map[string]*domain.Sector)
	for i, c := range constituents {
		p, pc := lookups[i].profile, lookups[i].change
		if p == nil || p.MarketCap <= 0 {
			stats.missingProfiles++ // without a market cap the tile has no size
			continue
		}
		// The provider's own lists carry these; the bundled lists do not.
		name, sectorName, subSector := c.Name, c.Sector, c.SubSector
		if name == "" {
			name = p.CompanyName
		}
		if sectorName == "" {
			sectorName = p.Sector
		}
		if subSector == "" {
			subSector = p.Industry
		}
		if sectorName == "" {
			sectorName = otherSector
		}
		sec, ok := bySector[sectorName]
		if !ok {
			sec = &domain.Sector{Name: sectorName}
			bySector[sectorName] = sec
		}
		var change domain.Change
		if pc != nil {
			// Copies, so the snapshot never aliases a cached PriceChange.
			d1, w1, m1, ytd := pc.D1, pc.D5, pc.M1, pc.YTD
			change = domain.Change{D1: &d1, W1: &w1, M1: &m1, YTD: &ytd}
		} else {
			stats.missingChanges++
		}
		sec.Stocks = append(sec.Stocks, domain.Stock{
			Symbol:    c.Symbol,
			Name:      name,
			SubSector: subSector,
			MarketCap: p.MarketCap,
			Price:     p.Price,
			Change:    change,
		})
		sec.MarketCap += p.MarketCap
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

// fetch looks up each symbol's profile and price change. A failed lookup leaves
// that part nil (the caller skips the stock or leaves its periods empty) rather
// than failing the whole map; one such error is returned as a sample for
// logging. While it runs it emits EventProgress every progressEvery.
func (uc *HeatmapUseCase) fetch(ctx context.Context, index domain.Index, start time.Time, symbols []string) (lookups []lookup, sampleErr, err error) {
	results := make([]lookup, len(symbols))
	var done atomic.Int64
	var firstErr atomic.Pointer[error]
	record := func(sym string, err error) {
		err = fmt.Errorf("%s: %w", sym, err)
		firstErr.CompareAndSwap(nil, &err)
	}

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
	g.SetLimit(fetchConcurrency)
	for i, sym := range symbols {
		if gctx.Err() != nil {
			break
		}
		g.Go(func() error {
			if gctx.Err() != nil {
				return gctx.Err()
			}
			defer done.Add(1)
			p, err := uc.profiles.GetProfile(sym)
			if err != nil {
				record(sym, fmt.Errorf("profile: %w", err))
				return nil // no size, so skip the price change too
			}
			results[i].profile = p
			if pc, err := uc.changer.GetPriceChange(sym); err != nil {
				record(sym, fmt.Errorf("price change: %w", err))
			} else {
				results[i].change = pc
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if p := firstErr.Load(); p != nil {
		sampleErr = *p
	}
	return results, sampleErr, nil
}
