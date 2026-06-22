package stockuc_test

import (
	"errors"
	"testing"

	domain "github.com/kanitin/stackvest/backend/internal/domain/stock"
	stockuc "github.com/kanitin/stackvest/backend/internal/usecase/stock"
)

type mockProfileFetcher struct {
	profile *domain.CompanyProfile
	err     error
}

func (m *mockProfileFetcher) GetProfile(string) (*domain.CompanyProfile, error) {
	return m.profile, m.err
}

func TestProfileUseCase_Execute(t *testing.T) {
	tests := []struct {
		name    string
		profile *domain.CompanyProfile
		err     error
		wantErr error
	}{
		{
			name:    "success",
			profile: &domain.CompanyProfile{Symbol: "AAPL", CompanyName: "Apple Inc."},
		},
		{
			name:    "symbol not found",
			err:     domain.ErrSymbolNotFound,
			wantErr: domain.ErrSymbolNotFound,
		},
		{
			name:    "upstream error",
			err:     errors.New("upstream error"),
			wantErr: errors.New("upstream error"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			uc := stockuc.NewProfileUseCase(&mockProfileFetcher{profile: tc.profile, err: tc.err})
			got, err := uc.Execute("AAPL")

			if tc.wantErr != nil {
				if err == nil || err.Error() != tc.wantErr.Error() {
					t.Fatalf("expected error %v, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.profile {
				t.Fatalf("expected profile %+v, got %+v", tc.profile, got)
			}
		})
	}
}
