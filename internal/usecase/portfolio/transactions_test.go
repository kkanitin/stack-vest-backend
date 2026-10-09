package portfolio_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	portfoliodomain "github.com/kanitin/stackvest/backend/internal/domain/portfolio"
	userdomain "github.com/kanitin/stackvest/backend/internal/domain/user"
	portfoliouc "github.com/kanitin/stackvest/backend/internal/usecase/portfolio"
)

func ownedRepo() *mockRepo {
	return &mockRepo{getPortfolio: func(id string) (*portfoliodomain.Portfolio, error) {
		return &portfoliodomain.Portfolio{ID: id, UserID: "u1"}, nil
	}}
}

func txUC(repo *mockRepo) *portfoliouc.UseCase {
	return newUC(repo, &mockUserRepo{user: &userdomain.User{ID: "u1"}}, 10, 20)
}

func validInput() portfoliouc.TransactionInput {
	return portfoliouc.TransactionInput{
		Symbol: "aapl", Name: "Apple", Side: "buy", Quantity: 5, Price: 190, Date: "2024-03-01",
	}
}

func TestCreateTransaction_NotOwned(t *testing.T) {
	repo := &mockRepo{getPortfolio: func(id string) (*portfoliodomain.Portfolio, error) {
		return &portfoliodomain.Portfolio{ID: id, UserID: "someone-else"}, nil
	}}
	_, err := txUC(repo).CreateTransaction(context.Background(), "a@b.com", "pf1", validInput())
	if !errors.Is(err, portfoliodomain.ErrPortfolioNotFound) {
		t.Fatalf("expected ErrPortfolioNotFound, got %v", err)
	}
	if repo.createCalls != 0 {
		t.Fatal("repo must not be called for a portfolio the user does not own")
	}
}

func TestCreateTransaction_Validation(t *testing.T) {
	future := time.Now().UTC().AddDate(0, 0, 2).Format(portfoliodomain.DateLayout)
	tests := []struct {
		name    string
		mutate  func(*portfoliouc.TransactionInput)
		wantErr error
	}{
		{"future date", func(in *portfoliouc.TransactionInput) { in.Date = future }, portfoliodomain.ErrFutureDate},
		{"bad date", func(in *portfoliouc.TransactionInput) { in.Date = "03/01/2024" }, portfoliodomain.ErrInvalidTransaction},
		{"bad side", func(in *portfoliouc.TransactionInput) { in.Side = "hold" }, portfoliodomain.ErrInvalidTransaction},
		{"zero quantity", func(in *portfoliouc.TransactionInput) { in.Quantity = 0 }, portfoliodomain.ErrInvalidTransaction},
		{"negative price", func(in *portfoliouc.TransactionInput) { in.Price = -1 }, portfoliodomain.ErrInvalidTransaction},
		{"negative fee", func(in *portfoliouc.TransactionInput) { in.Fee = -1 }, portfoliodomain.ErrInvalidTransaction},
		{"empty symbol", func(in *portfoliouc.TransactionInput) { in.Symbol = "  " }, portfoliodomain.ErrInvalidTransaction},
		{"quantity overflows NUMERIC", func(in *portfoliouc.TransactionInput) { in.Quantity = 1e12 }, portfoliodomain.ErrInvalidTransaction},
		{"price overflows NUMERIC", func(in *portfoliouc.TransactionInput) { in.Price = 1e12 }, portfoliodomain.ErrInvalidTransaction},
		{"fee overflows NUMERIC", func(in *portfoliouc.TransactionInput) { in.Fee = 1e12 }, portfoliodomain.ErrInvalidTransaction},
		{"infinite quantity", func(in *portfoliouc.TransactionInput) { in.Quantity = math.Inf(1) }, portfoliodomain.ErrInvalidTransaction},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := ownedRepo()
			in := validInput()
			tc.mutate(&in)
			_, err := txUC(repo).CreateTransaction(context.Background(), "a@b.com", "pf1", in)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
			if repo.createCalls != 0 {
				t.Fatal("repo must not be called for an invalid transaction")
			}
		})
	}
}

func TestCreateTransaction_PassesNormalisedRowAndLimit(t *testing.T) {
	var got *portfoliodomain.Transaction
	var gotMax int
	repo := ownedRepo()
	repo.createTransaction = func(pf string, tx *portfoliodomain.Transaction, max int) (*portfoliodomain.Transaction, *portfoliodomain.Position, error) {
		got, gotMax = tx, max
		out := *tx
		out.ID = "tx1"
		out.RunningShares = 5
		return &out, &portfoliodomain.Position{Symbol: tx.Symbol, Shares: 5, AvgCost: 190}, nil
	}
	res, err := txUC(repo).CreateTransaction(context.Background(), "a@b.com", "pf1", validInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Symbol != "AAPL" || got.Side != "buy" || got.Date != "2024-03-01" || got.Fee != 0 || got.IsOpening || got.PortfolioID != "pf1" {
		t.Fatalf("unexpected row passed to repo: %+v", got)
	}
	if gotMax != 20 {
		t.Fatalf("expected the position limit (20) to be passed, got %d", gotMax)
	}
	if res.Transaction.ID != "tx1" || res.Position == nil || res.Position.CostBasis != 950 || res.Position.Closed {
		t.Fatalf("unexpected result: %+v / %+v", res.Transaction, res.Position)
	}
}

func TestCreateTransaction_RepoErrorsPassThrough(t *testing.T) {
	for _, want := range []error{
		portfoliodomain.ErrPositionLimitReached,
		portfoliodomain.ErrInsufficientShares{Symbol: "AAPL", Date: "2024-03-01", Held: 3, Sold: 5},
	} {
		repo := ownedRepo()
		repo.createTransaction = func(string, *portfoliodomain.Transaction, int) (*portfoliodomain.Transaction, *portfoliodomain.Position, error) {
			return nil, nil, want
		}
		in := validInput()
		in.Side = "sell"
		_, err := txUC(repo).CreateTransaction(context.Background(), "a@b.com", "pf1", in)
		var ins portfoliodomain.ErrInsufficientShares
		if errors.Is(want, portfoliodomain.ErrPositionLimitReached) {
			if !errors.Is(err, want) {
				t.Fatalf("expected %v, got %v", want, err)
			}
		} else if !errors.As(err, &ins) || ins.Held != 3 {
			t.Fatalf("expected ErrInsufficientShares, got %v", err)
		}
	}
}

func TestAddPosition_RecordsBuyDatedToday(t *testing.T) {
	var got *portfoliodomain.Transaction
	repo := ownedRepo()
	repo.createTransaction = func(pf string, tx *portfoliodomain.Transaction, _ int) (*portfoliodomain.Transaction, *portfoliodomain.Position, error) {
		got = tx
		return tx, &portfoliodomain.Position{Symbol: tx.Symbol, Shares: tx.Quantity, AvgCost: tx.Price}, nil
	}
	pos, err := txUC(repo).AddPosition(context.Background(), "a@b.com", "pf1", "AAPL", "Apple", 2, 150)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	today := time.Now().UTC().Format(portfoliodomain.DateLayout)
	if got.Side != "buy" || got.Quantity != 2 || got.Price != 150 || got.Fee != 0 || got.Date != today {
		t.Fatalf("expected a buy dated %s, got %+v", today, got)
	}
	if pos.Shares != 2 || pos.AvgCost != 150 {
		t.Fatalf("unexpected position %+v", pos)
	}
}

func TestUpdatePosition_Gone(t *testing.T) {
	_, err := txUC(ownedRepo()).UpdatePosition(context.Background(), "a@b.com", "pf1", "AAPL")
	if !errors.Is(err, portfoliouc.ErrHoldingEditGone) {
		t.Fatalf("expected ErrHoldingEditGone, got %v", err)
	}
}

func existingTx() *portfoliodomain.Transaction {
	note := "first"
	return &portfoliodomain.Transaction{
		ID: "tx1", PortfolioID: "pf1", Symbol: "AAPL", Name: "Apple", Side: "buy",
		Quantity: 10, Price: 100, Fee: 1, Note: &note, Date: "2024-01-02",
	}
}

func TestUpdateTransaction_MergesPatch(t *testing.T) {
	var sent *portfoliodomain.Transaction
	repo := ownedRepo()
	repo.getTransaction = func(string, string) (*portfoliodomain.Transaction, error) { return existingTx(), nil }
	repo.updateTransaction = func(tx *portfoliodomain.Transaction) (*portfoliodomain.Transaction, *portfoliodomain.Position, error) {
		sent = tx
		return tx, &portfoliodomain.Position{Symbol: tx.Symbol, Shares: 8, AvgCost: 100}, nil
	}
	qty, price := 8.0, 105.0
	res, err := txUC(repo).UpdateTransaction(context.Background(), "a@b.com", "pf1", "tx1",
		portfoliouc.TransactionPatch{Quantity: &qty, Price: &price})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sent.Quantity != 8 || sent.Price != 105 || sent.Fee != 1 || sent.Side != "buy" || sent.Date != "2024-01-02" ||
		sent.Symbol != "AAPL" || sent.PortfolioID != "pf1" || sent.ID != "tx1" || sent.Note == nil || *sent.Note != "first" {
		t.Fatalf("patch not merged onto the stored row: %+v", sent)
	}
	if res.Position == nil {
		t.Fatal("expected a position in the result")
	}
}

func TestUpdateTransaction_Errors(t *testing.T) {
	future := time.Now().UTC().AddDate(0, 0, 3).Format(portfoliodomain.DateLayout)
	badSide, zero := "hold", 0.0

	t.Run("missing transaction", func(t *testing.T) {
		_, err := txUC(ownedRepo()).UpdateTransaction(context.Background(), "a@b.com", "pf1", "nope", portfoliouc.TransactionPatch{Date: &future})
		if !errors.Is(err, portfoliodomain.ErrTransactionNotFound) {
			t.Fatalf("expected ErrTransactionNotFound, got %v", err)
		}
	})
	repo := ownedRepo()
	repo.getTransaction = func(string, string) (*portfoliodomain.Transaction, error) { return existingTx(), nil }
	t.Run("future date", func(t *testing.T) {
		_, err := txUC(repo).UpdateTransaction(context.Background(), "a@b.com", "pf1", "tx1", portfoliouc.TransactionPatch{Date: &future})
		if !errors.Is(err, portfoliodomain.ErrFutureDate) {
			t.Fatalf("expected ErrFutureDate, got %v", err)
		}
	})
	t.Run("bad side", func(t *testing.T) {
		_, err := txUC(repo).UpdateTransaction(context.Background(), "a@b.com", "pf1", "tx1", portfoliouc.TransactionPatch{Side: &badSide})
		if !errors.Is(err, portfoliodomain.ErrInvalidTransaction) {
			t.Fatalf("expected ErrInvalidTransaction, got %v", err)
		}
	})
	t.Run("zero quantity", func(t *testing.T) {
		_, err := txUC(repo).UpdateTransaction(context.Background(), "a@b.com", "pf1", "tx1", portfoliouc.TransactionPatch{Quantity: &zero})
		if !errors.Is(err, portfoliodomain.ErrInvalidTransaction) {
			t.Fatalf("expected ErrInvalidTransaction, got %v", err)
		}
	})
	t.Run("oversell from repo", func(t *testing.T) {
		repo.updateTransaction = func(*portfoliodomain.Transaction) (*portfoliodomain.Transaction, *portfoliodomain.Position, error) {
			return nil, nil, portfoliodomain.ErrInsufficientShares{Symbol: "AAPL", Date: "2024-02-01", Held: 3, Sold: 5}
		}
		q := 1.0
		_, err := txUC(repo).UpdateTransaction(context.Background(), "a@b.com", "pf1", "tx1", portfoliouc.TransactionPatch{Quantity: &q})
		if !errors.As(err, new(portfoliodomain.ErrInsufficientShares)) {
			t.Fatalf("expected ErrInsufficientShares, got %v", err)
		}
	})
	t.Run("not owned", func(t *testing.T) {
		foreign := &mockRepo{getPortfolio: func(id string) (*portfoliodomain.Portfolio, error) {
			return &portfoliodomain.Portfolio{ID: id, UserID: "x"}, nil
		}}
		_, err := txUC(foreign).UpdateTransaction(context.Background(), "a@b.com", "pf1", "tx1", portfoliouc.TransactionPatch{})
		if !errors.Is(err, portfoliodomain.ErrPortfolioNotFound) {
			t.Fatalf("expected ErrPortfolioNotFound, got %v", err)
		}
	})
}

func TestDeleteTransaction(t *testing.T) {
	var gotPf, gotTx string
	repo := ownedRepo()
	repo.deleteTransaction = func(pf, tx string) error { gotPf, gotTx = pf, tx; return nil }
	if err := txUC(repo).DeleteTransaction(context.Background(), "a@b.com", "pf1", "tx9"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPf != "pf1" || gotTx != "tx9" {
		t.Fatalf("unexpected ids %s/%s", gotPf, gotTx)
	}

	repo.deleteTransaction = func(string, string) error {
		return portfoliodomain.ErrInsufficientShares{Symbol: "AAPL", Date: "2024-02-01", Held: 0, Sold: 4}
	}
	err := txUC(repo).DeleteTransaction(context.Background(), "a@b.com", "pf1", "tx9")
	if !errors.As(err, new(portfoliodomain.ErrInsufficientShares)) {
		t.Fatalf("expected ErrInsufficientShares, got %v", err)
	}

	foreign := &mockRepo{getPortfolio: func(id string) (*portfoliodomain.Portfolio, error) {
		return &portfoliodomain.Portfolio{ID: id, UserID: "x"}, nil
	}}
	if err := txUC(foreign).DeleteTransaction(context.Background(), "a@b.com", "pf1", "tx9"); !errors.Is(err, portfoliodomain.ErrPortfolioNotFound) {
		t.Fatalf("expected ErrPortfolioNotFound, got %v", err)
	}
}

// ledger returns AAPL: buy 10 @100 (Jan), sell 4 @120 (Feb); MSFT: buy 2 @300 (Mar), in
// the repo's newest-first order.
func ledger() []*portfoliodomain.Transaction {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	return []*portfoliodomain.Transaction{
		{ID: "m1", Symbol: "MSFT", Side: "buy", Quantity: 2, Price: 300, Date: "2024-03-01", CreatedAt: base.AddDate(0, 2, 0)},
		{ID: "a2", Symbol: "AAPL", Side: "sell", Quantity: 4, Price: 120, Date: "2024-02-01", CreatedAt: base.AddDate(0, 1, 0)},
		{ID: "a1", Symbol: "AAPL", Side: "buy", Quantity: 10, Price: 100, Date: "2024-01-02", CreatedAt: base},
	}
}

func TestListTransactions_FillsRunningSharesAndRealisedPnl(t *testing.T) {
	repo := ownedRepo()
	repo.listTransactions = func(string, string) ([]*portfoliodomain.Transaction, error) { return ledger(), nil }

	txs, total, err := txUC(repo).ListTransactions(context.Background(), "a@b.com", "pf1", "", 1, 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 3 || len(txs) != 3 {
		t.Fatalf("expected 3 transactions, got total=%d len=%d", total, len(txs))
	}
	if txs[0].ID != "m1" || txs[1].ID != "a2" || txs[2].ID != "a1" {
		t.Fatalf("expected the repo's newest-first order to be kept, got %s %s %s", txs[0].ID, txs[1].ID, txs[2].ID)
	}
	if txs[0].RunningShares != 2 || txs[0].RealisedPnl != nil {
		t.Fatalf("MSFT buy: %+v", txs[0])
	}
	if txs[1].RunningShares != 6 || txs[1].RealisedPnl == nil || *txs[1].RealisedPnl != 80 {
		t.Fatalf("AAPL sell: running=%v pnl=%v (want 6, 80)", txs[1].RunningShares, txs[1].RealisedPnl)
	}
	if txs[2].RunningShares != 10 || txs[2].RealisedPnl != nil {
		t.Fatalf("AAPL buy: %+v", txs[2])
	}
}

func TestListTransactions_PaginatesAndFiltersBySymbol(t *testing.T) {
	var gotSymbol string
	repo := ownedRepo()
	repo.listTransactions = func(_, symbol string) ([]*portfoliodomain.Transaction, error) {
		gotSymbol = symbol
		return ledger(), nil
	}
	txs, total, err := txUC(repo).ListTransactions(context.Background(), "a@b.com", "pf1", "AAPL", 2, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotSymbol != "AAPL" || total != 3 || len(txs) != 1 || txs[0].ID != "a1" {
		t.Fatalf("symbol=%q total=%d page=%v", gotSymbol, total, txs)
	}
	txs, _, _ = txUC(repo).ListTransactions(context.Background(), "a@b.com", "pf1", "", 9, 2)
	if len(txs) != 0 {
		t.Fatalf("a page past the end must be empty, got %d", len(txs))
	}
}

func TestListTransactions_NotOwned(t *testing.T) {
	repo := &mockRepo{getPortfolio: func(id string) (*portfoliodomain.Portfolio, error) {
		return &portfoliodomain.Portfolio{ID: id, UserID: "x"}, nil
	}}
	_, _, err := txUC(repo).ListTransactions(context.Background(), "a@b.com", "pf1", "", 1, 20)
	if !errors.Is(err, portfoliodomain.ErrPortfolioNotFound) {
		t.Fatalf("expected ErrPortfolioNotFound, got %v", err)
	}
}

func TestListPositions_UnrealisedPnlFromLivePrice(t *testing.T) {
	repo := ownedRepo()
	repo.listByPortfolioID = func(string) ([]*portfoliodomain.Position, error) {
		return []*portfoliodomain.Position{
			{Symbol: "AAPL", Shares: 10, AvgCost: 100, RealisedPnl: 5},
			{Symbol: "MSFT", Shares: 0, AvgCost: 0, RealisedPnl: 50}, // closed
		}, nil
	}
	uc := newUCWithPrices(repo, &mockUserRepo{user: &userdomain.User{ID: "u1"}},
		stubQuoter{price: map[string]float64{"AAPL": 120, "MSFT": 400}}, stubPriceChanger{})

	positions, err := uc.ListPositions(context.Background(), "a@b.com", "pf1", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !repo.lastIncludeClosed {
		t.Fatal("includeClosed must be forwarded to the repo")
	}
	open, closed := positions[0], positions[1]
	if open.CostBasis != 1000 || open.ValueUsd != 1200 || open.UnrealisedPnl != 200 || open.UnrealisedPnlPct != 20 || open.Closed {
		t.Fatalf("open position: %+v", open)
	}
	if !closed.Closed || closed.ValueUsd != 0 || closed.UnrealisedPnl != 0 || closed.RealisedPnl != 50 {
		t.Fatalf("closed position must carry no value or unrealised P&L: %+v", closed)
	}

	if _, err := uc.ListPositions(context.Background(), "a@b.com", "pf1", false); err != nil {
		t.Fatal(err)
	}
	if repo.lastIncludeClosed {
		t.Fatal("includeClosed=false must be forwarded as false")
	}
}

func TestListPortfolios_ClosedRowsNotCounted(t *testing.T) {
	repo := &mockRepo{
		listPortfolios: func(string) ([]*portfoliodomain.Portfolio, error) {
			return []*portfoliodomain.Portfolio{{ID: "pf1", UserID: "u1"}}, nil
		},
		listPositionsByUser: func(string) ([]*portfoliodomain.Position, error) {
			return []*portfoliodomain.Position{
				{PortfolioID: "pf1", Symbol: "AAPL", Shares: 2},
				{PortfolioID: "pf1", Symbol: "MSFT", Shares: 0, Closed: true},
			}, nil
		},
	}
	q := stubQuoter{price: map[string]float64{"AAPL": 100, "MSFT": 400}}
	uc := newUCWithPrices(repo, &mockUserRepo{user: &userdomain.User{ID: "u1"}}, q, stubPriceChanger{})

	ps, err := uc.ListPortfolios(context.Background(), "a@b.com")
	if err != nil {
		t.Fatal(err)
	}
	if ps[0].AssetCount == nil || *ps[0].AssetCount != 1 {
		t.Fatalf("a closed row must not count as an asset, got %v", ps[0].AssetCount)
	}
	if ps[0].Value == nil || *ps[0].Value != 200 {
		t.Fatalf("expected value 200, got %v", ps[0].Value)
	}
}

// An opening entry (carried over from a pre-ledger holding) replays to the stored holding,
// and selling it all leaves a closed row.
func TestListTransactions_OpeningEntryThenFullSell(t *testing.T) {
	repo := ownedRepo()
	repo.listTransactions = func(string, string) ([]*portfoliodomain.Transaction, error) {
		return []*portfoliodomain.Transaction{
			{ID: "s", Symbol: "AAPL", Side: "sell", Quantity: 3, Price: 150, Date: "2024-02-01", CreatedAt: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)},
			{ID: "o", Symbol: "AAPL", Side: "buy", Quantity: 3, Price: 100, Date: "2024-01-01", IsOpening: true, CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
		}, nil
	}
	txs, _, err := txUC(repo).ListTransactions(context.Background(), "a@b.com", "pf1", "AAPL", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if txs[1].RunningShares != 3 || txs[0].RunningShares != 0 || txs[0].RealisedPnl == nil || *txs[0].RealisedPnl != 150 {
		t.Fatalf("unexpected replay: opening=%+v sell=%+v", txs[1], txs[0])
	}
}

func TestUpdateTransaction_RejectsOverflowingNumbers(t *testing.T) {
	repo := ownedRepo()
	repo.getTransaction = func(string, string) (*portfoliodomain.Transaction, error) { return existingTx(), nil }
	big := 1e12
	for name, patch := range map[string]portfoliouc.TransactionPatch{
		"quantity": {Quantity: &big}, "price": {Price: &big}, "fee": {Fee: &big},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := txUC(repo).UpdateTransaction(context.Background(), "a@b.com", "pf1", "tx1", patch)
			if !errors.Is(err, portfoliodomain.ErrInvalidTransaction) {
				t.Fatalf("expected ErrInvalidTransaction, got %v", err)
			}
		})
	}
}

func TestUpdateTransaction_PassesPatchToRepoForLockedMerge(t *testing.T) {
	repo := ownedRepo()
	repo.getTransaction = func(string, string) (*portfoliodomain.Transaction, error) { return existingTx(), nil }
	var gotPf, gotTx string
	var gotPatch portfoliodomain.TransactionPatch
	repo.updateTransactionPatch = func(pf, tx string, p portfoliodomain.TransactionPatch) {
		gotPf, gotTx, gotPatch = pf, tx, p
	}
	qty := 3.0
	if _, err := txUC(repo).UpdateTransaction(context.Background(), "a@b.com", "pf1", "tx1", portfoliouc.TransactionPatch{Quantity: &qty}); err != nil {
		t.Fatal(err)
	}
	if gotPf != "pf1" || gotTx != "tx1" || gotPatch.Quantity == nil || *gotPatch.Quantity != 3 || gotPatch.Price != nil {
		t.Fatalf("repo got %q %q %+v", gotPf, gotTx, gotPatch)
	}
}

func TestAddPosition_OpenPositionAlreadyExists(t *testing.T) {
	repo := ownedRepo()
	repo.listByPortfolioID = func(string) ([]*portfoliodomain.Position, error) {
		return []*portfoliodomain.Position{{PortfolioID: "pf1", Symbol: "AAPL", Shares: 3}}, nil
	}
	_, err := txUC(repo).AddPosition(context.Background(), "a@b.com", "pf1", " aapl ", "Apple", 1, 100)
	if !errors.Is(err, portfoliodomain.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}
	if repo.createCalls != 0 {
		t.Fatal("no transaction may be recorded for a duplicate")
	}
}

func TestAddPosition_ClosedPositionMayReopen(t *testing.T) {
	repo := ownedRepo()
	repo.listByPortfolioID = func(string) ([]*portfoliodomain.Position, error) {
		return []*portfoliodomain.Position{{PortfolioID: "pf1", Symbol: "AAPL", Shares: 0, Closed: true}}, nil
	}
	if _, err := txUC(repo).AddPosition(context.Background(), "a@b.com", "pf1", "AAPL", "Apple", 1, 100); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.createCalls != 1 {
		t.Fatalf("createCalls = %d", repo.createCalls)
	}
}

func TestAddPosition_BlankSymbolOrNameIsInvalid(t *testing.T) {
	for _, tc := range [][2]string{{"   ", "Apple"}, {"AAPL", "  "}} {
		repo := ownedRepo()
		_, err := txUC(repo).AddPosition(context.Background(), "a@b.com", "pf1", tc[0], tc[1], 1, 100)
		if !errors.Is(err, portfoliodomain.ErrInvalidTransaction) {
			t.Fatalf("%q/%q: expected ErrInvalidTransaction, got %v", tc[0], tc[1], err)
		}
	}
}

func TestUpdateAndDeleteTransaction_PassPositionLimitToRepo(t *testing.T) {
	// Edits and deletes can reopen a closed symbol, so the repo needs the limit to refuse
	// that when the portfolio is full (newUC(..., 10, 20): maxPositions is the last).
	repo := ownedRepo()
	repo.getTransaction = func(string, string) (*portfoliodomain.Transaction, error) { return existingTx(), nil }
	uc := txUC(repo)
	qty := 2.0
	if _, err := uc.UpdateTransaction(context.Background(), "a@b.com", "pf1", "tx1", portfoliouc.TransactionPatch{Quantity: &qty}); err != nil {
		t.Fatal(err)
	}
	updateMax := repo.lastMaxPositions
	if err := uc.DeleteTransaction(context.Background(), "a@b.com", "pf1", "tx1"); err != nil {
		t.Fatal(err)
	}
	if updateMax != 20 || repo.lastMaxPositions != 20 {
		t.Fatalf("limit passed to repo: update=%d delete=%d, want 20", updateMax, repo.lastMaxPositions)
	}
}
