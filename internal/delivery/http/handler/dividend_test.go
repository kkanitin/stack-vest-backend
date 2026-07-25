package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	dividenddomain "github.com/kanitin/stackvest/backend/internal/domain/dividend"
)

type mockDividendCalendarUC struct {
	entries []dividenddomain.CalendarEntry
	err     error
}

func (m *mockDividendCalendarUC) Execute(ctx context.Context, email string, from, to time.Time) ([]dividenddomain.CalendarEntry, error) {
	return m.entries, m.err
}

func newDividendRouter(uc dividendCalendarUseCase) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewDividendHandler(uc).RegisterRoutes(r.Group(""))
	return r
}

func makeEntries(n int) []dividenddomain.CalendarEntry {
	entries := make([]dividenddomain.CalendarEntry, n)
	for i := range entries {
		entries[i].Symbol = fmt.Sprintf("SYM%03d", i)
	}
	return entries
}

func TestGetCalendar(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		entries  []dividenddomain.CalendarEntry
		mockErr  error
		wantCode int
	}{
		{"invalid from", "/dividends/calendar?from=not-a-date", nil, nil, http.StatusBadRequest},
		{"to before from", "/dividends/calendar?from=2026-02-01&to=2026-01-01", nil, nil, http.StatusBadRequest},
		{"size too large", "/dividends/calendar?size=101", nil, nil, http.StatusBadRequest},
		{"page too small", "/dividends/calendar?page=0", nil, nil, http.StatusBadRequest},
		{"page huge", "/dividends/calendar?page=9223372036854775807&size=100", nil, nil, http.StatusBadRequest},
		{"use-case error", "/dividends/calendar", nil, errors.New("upstream error"), http.StatusInternalServerError},
		{"success", "/dividends/calendar", makeEntries(3), nil, http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newDividendRouter(&mockDividendCalendarUC{entries: tc.entries, err: tc.mockErr})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.url, nil))
			if w.Code != tc.wantCode {
				t.Fatalf("expected %d, got %d", tc.wantCode, w.Code)
			}
		})
	}
}

type calendarMeta struct {
	Meta struct {
		Total            *int `json:"total"`
		Page             *int `json:"page"`
		Size             *int `json:"size"`
		CurrentPageCount *int `json:"currentPageCount"`
	} `json:"meta"`
	Results []dividenddomain.CalendarEntry `json:"results"`
}

func TestGetCalendarPagination(t *testing.T) {
	tests := []struct {
		name             string
		url              string
		total            int
		wantPage         int
		wantSize         int
		wantCurrentCount int
		wantFirstSymbol  string
	}{
		{"defaults", "/dividends/calendar", 25, 1, 20, 20, "SYM000"},
		{"second page", "/dividends/calendar?page=2&size=5", 12, 2, 5, 5, "SYM005"},
		{"last partial page", "/dividends/calendar?page=3&size=5", 12, 3, 5, 2, "SYM010"},
		{"page beyond range", "/dividends/calendar?page=99&size=5", 12, 99, 5, 0, ""},
		{"empty result", "/dividends/calendar", 0, 1, 20, 0, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newDividendRouter(&mockDividendCalendarUC{entries: makeEntries(tc.total)})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.url, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", w.Code)
			}

			var body calendarMeta
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			m := body.Meta
			if m.Total == nil || m.Page == nil || m.Size == nil || m.CurrentPageCount == nil {
				t.Fatalf("all four Meta fields must be populated, got %+v", m)
			}
			if *m.Total != tc.total {
				t.Errorf("total: want %d, got %d", tc.total, *m.Total)
			}
			if *m.Page != tc.wantPage {
				t.Errorf("page: want %d, got %d", tc.wantPage, *m.Page)
			}
			if *m.Size != tc.wantSize {
				t.Errorf("size: want %d, got %d", tc.wantSize, *m.Size)
			}
			if *m.CurrentPageCount != tc.wantCurrentCount {
				t.Errorf("currentPageCount: want %d, got %d", tc.wantCurrentCount, *m.CurrentPageCount)
			}
			if len(body.Results) != tc.wantCurrentCount {
				t.Errorf("results len: want %d, got %d", tc.wantCurrentCount, len(body.Results))
			}
			if tc.wantCurrentCount > 0 && body.Results[0].Symbol != tc.wantFirstSymbol {
				t.Errorf("first symbol: want %s, got %s", tc.wantFirstSymbol, body.Results[0].Symbol)
			}
		})
	}
}
