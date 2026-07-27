package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	fmp "github.com/kanitin/stackvest/backend/internal/infrastructure/fmp"

	"github.com/kanitin/stackvest/backend/internal/delivery/http/response"
	"github.com/kanitin/stackvest/backend/pkg/cache"
	"github.com/kanitin/stackvest/backend/pkg/logger"
)

// stockFetcher is implemented by *fmp.Client.
type stockFetcher interface {
	GetMostActiveStocks(n int) ([]fmp.MostActiveStock, error)
}

type PopularHandler struct {
	fmp        stockFetcher
	stockCache *cache.TTL[[]popularEntry]
}

func NewPopularHandler(fmpClient stockFetcher) *PopularHandler {
	return &PopularHandler{
		fmp:        fmpClient,
		stockCache: cache.NewTTLWithNegative[[]popularEntry](5*time.Minute, 30*time.Second),
	}
}

func (h *PopularHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/popular", h.list)
}

type popularEntry struct {
	Symbol   string   `json:"symbol"`
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Category []string `json:"category"`
}

var popularAssets = []popularEntry{
	{Symbol: "BTC", Name: "Bitcoin", Type: "crypto", Category: []string{"Top 100", "L1s"}},
	{Symbol: "ETH", Name: "Ethereum", Type: "crypto", Category: []string{"Top 100", "L1s"}},
	{Symbol: "BNB", Name: "BNB", Type: "crypto", Category: []string{"Top 100", "L1s"}},
	{Symbol: "SOL", Name: "Solana", Type: "crypto", Category: []string{"Top 100", "L1s"}},
	{Symbol: "XRP", Name: "XRP", Type: "crypto", Category: []string{"Top 100"}},
	{Symbol: "ADA", Name: "Cardano", Type: "crypto", Category: []string{"Top 100", "L1s"}},
	{Symbol: "AVAX", Name: "Avalanche", Type: "crypto", Category: []string{"Top 100", "L1s"}},
	{Symbol: "DOT", Name: "Polkadot", Type: "crypto", Category: []string{"Top 100", "L1s"}},
	{Symbol: "MATIC", Name: "Polygon", Type: "crypto", Category: []string{"Top 100", "L1s"}},
	{Symbol: "NEAR", Name: "NEAR Protocol", Type: "crypto", Category: []string{"Top 100", "L1s"}},
	{Symbol: "ATOM", Name: "Cosmos", Type: "crypto", Category: []string{"Top 100", "L1s"}},
	{Symbol: "ARB", Name: "Arbitrum", Type: "crypto", Category: []string{"Top 100", "L1s"}},
	{Symbol: "OP", Name: "Optimism", Type: "crypto", Category: []string{"Top 100", "L1s"}},
	{Symbol: "UNI", Name: "Uniswap", Type: "crypto", Category: []string{"Top 100", "DeFi"}},
	{Symbol: "AAVE", Name: "Aave", Type: "crypto", Category: []string{"Top 100", "DeFi"}},
	{Symbol: "LINK", Name: "Chainlink", Type: "crypto", Category: []string{"Top 100"}},
	{Symbol: "LTC", Name: "Litecoin", Type: "crypto", Category: []string{"Top 100"}},
	{Symbol: "DOGE", Name: "Dogecoin", Type: "crypto", Category: []string{"Top 100"}},
	{Symbol: "MKR", Name: "Maker", Type: "crypto", Category: []string{"Top 100", "DeFi"}},
	{Symbol: "CRV", Name: "Curve DAO Token", Type: "crypto", Category: []string{"Top 100", "DeFi"}},
}

func (h *PopularHandler) list(c *gin.Context) {
	assetType := c.DefaultQuery("type", "crypto")

	var limit int
	if raw := c.Query("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > 50 {
			response.Err(c, http.StatusBadRequest, "limit must be an integer between 1 and 50")
			return
		}
		limit = v
	}

	var results []popularEntry

	switch assetType {
	case "crypto":
		results = popularAssets

	case "stock":
		stocks, err := h.getStocks(c)
		if err != nil {
			// FMP unavailable — return empty list, not a 500
			results = []popularEntry{}
		} else {
			results = stocks
		}

	case "all":
		stocks, _ := h.getStocks(c) // error → empty stocks; crypto is still returned
		if limit > 0 {
			// 60/40 proportional split: ceiling(limit * 0.6) crypto, remainder stocks
			cryptoN := (limit*6 + 9) / 10
			if cryptoN > len(popularAssets) {
				cryptoN = len(popularAssets)
			}
			stockN := limit - cryptoN
			if stockN > len(stocks) {
				stockN = len(stocks)
			}
			results = make([]popularEntry, 0, cryptoN+stockN)
			results = append(results, popularAssets[:cryptoN]...)
			results = append(results, stocks[:stockN]...)
		} else {
			results = make([]popularEntry, 0, len(popularAssets)+len(stocks))
			results = append(results, popularAssets...)
			results = append(results, stocks...)
		}

	default:
		response.Err(c, http.StatusBadRequest, "type must be one of: stock, crypto, all")
		return
	}

	// Apply limit for stock and crypto (type=all already handles its own split above)
	if limit > 0 && assetType != "all" && limit < len(results) {
		results = results[:limit]
	}

	total := len(results)
	response.OKList(c, results, response.Meta{
		Total:            &total,
		CurrentPageCount: &total,
	})
}

// getStocks fills stockCache on a miss, coalescing concurrent misses into a
// single upstream call via TTL.Fill. A failed fetch is negative-cached for a
// short interval so an FMP outage doesn't re-hit it on every request; Fill
// reports healthy=false both for a fresh failure and for a later warm hit on
// that negative cache entry, so every caller (leader and coalesced followers
// alike) sees a consistent result.
func (h *PopularHandler) getStocks(c *gin.Context) ([]popularEntry, error) {
	if h.fmp == nil {
		return nil, fmt.Errorf("stock fetcher not configured")
	}

	entries, healthy := h.stockCache.Fill(func() ([]popularEntry, bool) {
		stocks, err := h.fmp.GetMostActiveStocks(50)
		if err != nil {
			zap.L().Warn("failed to fetch most-active stocks from FMP", logger.RequestID(c.Request.Context()), zap.Error(err))
			return nil, false
		}

		out := make([]popularEntry, 0, len(stocks))
		for _, s := range stocks {
			out = append(out, popularEntry{
				Symbol:   s.Symbol,
				Name:     s.Name,
				Type:     "stock",
				Category: []string{"Most Active"},
			})
		}
		return out, true
	})
	if !healthy {
		return nil, fmt.Errorf("most-active stocks unavailable")
	}
	return entries, nil
}

func (h *PopularHandler) GetPopularAssets() []popularEntry {
	return popularAssets
}
