// Command constituents regenerates the bundled S&P 500 member list used by the
// index heatmap when the FMP plan does not include the constituents endpoints.
//
//	go run ./cmd/constituents
//
// It reads the public datasets/s-and-p-500-companies CSV and writes one FMP
// symbol per line. Nasdaq 100 and Dow 30 have no equivalent public file, so
// their lists (nasdaq100.txt, dow30.txt next to the output) are edited by hand.
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const sourceURL = "https://raw.githubusercontent.com/datasets/s-and-p-500-companies/main/data/constituents.csv"

// secondaryShareClasses are dropped so a company is not drawn twice: FMP
// reports the whole company's market cap on each share class.
var secondaryShareClasses = map[string]bool{"GOOG": true, "FOX": true, "NWS": true}

func main() {
	out := flag.String("out", "internal/infrastructure/constituents/data/sp500.txt", "file to write")
	flag.Parse()
	if err := run(*out); err != nil {
		fmt.Fprintln(os.Stderr, "constituents:", err)
		os.Exit(1)
	}
}

func run(out string) error {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(sourceURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", sourceURL, resp.StatusCode)
	}

	rows, err := csv.NewReader(resp.Body).ReadAll()
	if err != nil {
		return fmt.Errorf("parse csv: %w", err)
	}
	if len(rows) < 2 || rows[0][0] != "Symbol" {
		return fmt.Errorf("unexpected csv header %v", rows[0])
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# S&P 500 members, generated %s by `go run ./cmd/constituents` from\n# %s\n", time.Now().UTC().Format("2006-01-02"), sourceURL)
	n := 0
	for _, row := range rows[1:] {
		// FMP writes share classes with a dash (BRK-B), the CSV with a dot (BRK.B).
		sym := strings.ReplaceAll(strings.TrimSpace(row[0]), ".", "-")
		if sym == "" || secondaryShareClasses[sym] {
			continue
		}
		b.WriteString(sym + "\n")
		n++
	}
	if n < 450 {
		return fmt.Errorf("only %d symbols parsed; refusing to write a truncated list", n)
	}
	if err := os.WriteFile(out, []byte(b.String()), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %d symbols to %s\n", n, out)
	return nil
}
