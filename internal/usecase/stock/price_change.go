package stockuc

import domain "github.com/kanitin/stackvest/backend/internal/domain/stock"

type PriceChangeUseCase struct {
	changer domain.PriceChanger
}

func NewPriceChangeUseCase(c domain.PriceChanger) *PriceChangeUseCase {
	return &PriceChangeUseCase{changer: c}
}

func (uc *PriceChangeUseCase) Execute(symbol string) (*domain.PriceChange, error) {
	if symbol == "" {
		return nil, nil
	}
	return uc.changer.GetPriceChange(symbol)
}
