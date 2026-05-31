package stockuc

import domain "github.com/kanitin/stackvest/backend/internal/domain/stock"

type QuoteUseCase struct {
	quoter domain.Quoter
}

func NewQuoteUseCase(q domain.Quoter) *QuoteUseCase {
	return &QuoteUseCase{quoter: q}
}

func (uc *QuoteUseCase) Execute(symbol string) (*domain.Quote, error) {
	return uc.quoter.GetQuote(symbol)
}
