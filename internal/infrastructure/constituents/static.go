// Package constituents supplies index membership for the heatmap when the
// market-data plan does not include the provider's constituents endpoints.
package constituents

import (
	"bufio"
	"embed"
	"fmt"
	"strings"

	"github.com/kanitin/stackvest/backend/internal/domain/market"
)

//go:embed data/*.txt
var files embed.FS

var indexFiles = map[market.Index]string{
	market.IndexSP500:     "data/sp500.txt",
	market.IndexNasdaq100: "data/nasdaq100.txt",
	market.IndexDow30:     "data/dow30.txt",
}

// StaticLister returns the bundled member lists (symbols only; name and sector
// come from each company's profile). Regenerate sp500.txt with
// `go run ./cmd/constituents`; the other two are maintained by hand.
type StaticLister struct{}

func (StaticLister) ListConstituents(index market.Index) ([]market.Constituent, error) {
	name, ok := indexFiles[index]
	if !ok {
		return nil, market.ErrUnknownIndex
	}
	data, err := files.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	var out []market.Constituent
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, market.Constituent{Symbol: line})
	}
	return out, nil
}

var _ market.ConstituentLister = StaticLister{}
