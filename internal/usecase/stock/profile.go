package stockuc

import domain "github.com/kanitin/stackvest/backend/internal/domain/stock"

type ProfileUseCase struct {
	fetcher domain.ProfileFetcher
}

func NewProfileUseCase(f domain.ProfileFetcher) *ProfileUseCase {
	return &ProfileUseCase{fetcher: f}
}

func (uc *ProfileUseCase) Execute(symbol string) (*domain.CompanyProfile, error) {
	return uc.fetcher.GetProfile(symbol)
}
