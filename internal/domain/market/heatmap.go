// Package market holds the index heatmap: every constituent of a stock index
// grouped by sector, with its market cap and performance over several periods.
package market

import (
	"context"
	"errors"
	"time"
)

var (
	ErrUnknownIndex = errors.New("unknown index")
	// ErrHeatmapNotReady is returned until the first snapshot of an index is built.
	ErrHeatmapNotReady = errors.New("heatmap not ready")
	// ErrPlanRestricted means the market-data plan does not include an endpoint.
	ErrPlanRestricted = errors.New("endpoint not available on the current data plan")
)

type Index string

const (
	IndexSP500     Index = "sp500"
	IndexNasdaq100 Index = "nasdaq100"
	IndexDow30     Index = "dow30"
)

// Indexes lists every supported index, smallest first so a cold start makes the
// cheap maps available before the S&P 500 finishes.
var Indexes = []Index{IndexDow30, IndexNasdaq100, IndexSP500}

func ParseIndex(s string) (Index, error) {
	for _, idx := range Indexes {
		if string(idx) == s {
			return idx, nil
		}
	}
	return "", ErrUnknownIndex
}

type Constituent struct {
	Symbol    string
	Name      string
	Sector    string
	SubSector string
}

// Quote is the slice of a live quote the heatmap needs.
type Quote struct {
	Symbol        string
	Price         float64
	ChangePercent float64
	MarketCap     float64
}

// Change holds percentage changes per period. A nil value means the period is
// unavailable for that symbol.
type Change struct {
	D1  *float64 `json:"1D"`
	W1  *float64 `json:"1W"`
	M1  *float64 `json:"1M"`
	YTD *float64 `json:"YTD"`
}

type Stock struct {
	Symbol    string  `json:"symbol"`
	Name      string  `json:"name"`
	SubSector string  `json:"subSector"`
	MarketCap float64 `json:"marketCap"`
	Price     float64 `json:"price"`
	Change    Change  `json:"change"`
}

type Sector struct {
	Name      string  `json:"name"`
	MarketCap float64 `json:"marketCap"`
	Stocks    []Stock `json:"stocks"`
}

type Heatmap struct {
	Index     Index     `json:"index"`
	UpdatedAt time.Time `json:"updatedAt"`
	Sectors   []Sector  `json:"sectors"`
}

type ConstituentLister interface {
	ListConstituents(index Index) ([]Constituent, error)
}

type BatchQuoter interface {
	// GetBatchQuotes returns quotes for the symbols it found; unknown symbols are omitted.
	GetBatchQuotes(symbols []string) ([]Quote, error)
}

// SnapshotStore persists built heatmaps so a restart can serve the last one
// immediately instead of waiting for a full rebuild.
type SnapshotStore interface {
	// Load returns the stored heatmap for index, or nil, nil when there is none.
	Load(ctx context.Context, index Index) (*Heatmap, error)
	Save(ctx context.Context, hm *Heatmap) error
}
