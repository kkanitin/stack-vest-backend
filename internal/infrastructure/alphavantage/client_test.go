package alphavantage

import "testing"

func TestNewClient(t *testing.T) {
	apiKey := "test-api-key"
	client := NewClient(apiKey)

	if client.apiKey != apiKey {
		t.Errorf("expected apiKey %s, got %s", apiKey, client.apiKey)
	}

	if client.baseURL != "https://www.alphavantage.co/query" {
		t.Errorf("expected baseURL https://www.alphavantage.co/query, got %s", client.baseURL)
	}
}

func TestGetStockQuoteSkeleton(t *testing.T) {
	client := NewClient("test-key")
	_, err := client.GetStockQuote("AAPL")
	if err != nil {
		t.Errorf("expected no error from skeleton, got %v", err)
	}
}
