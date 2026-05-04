package stockuc

import domain "github.com/kanitin/stackvest/backend/internal/domain/stock"

type SearchUseCase struct {
	searcher domain.Searcher
}

func NewSearchUseCase(s domain.Searcher) *SearchUseCase {
	return &SearchUseCase{searcher: s}
}

func (uc *SearchUseCase) Execute(keywords string) ([]domain.Match, error) {
	if keywords == "" {
		return []domain.Match{}, nil
	}
	return uc.searcher.SearchSymbol(keywords)
}
