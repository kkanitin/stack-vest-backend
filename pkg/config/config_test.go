package config

import (
	"os"
	"testing"
)

func TestLoad(t *testing.T) {
	// Create a temporary config.yaml
	content := []byte(`
server:
  port: "9090"
db:
  mongo:
    uri: "mongodb://test:27017"
third_party_api:
  alpha_vantage:
    api_key: "test-key"
`)
	err := os.WriteFile("config.yaml.test", content, 0644)
	if err != nil {
		t.Fatalf("failed to create test config file: %v", err)
	}
	defer os.Remove("config.yaml.test")

	// Temporarily rename the real config.yaml if it exists
	if _, err := os.Stat("config.yaml"); err == nil {
		os.Rename("config.yaml", "config.yaml.bak")
		defer os.Rename("config.yaml.bak", "config.yaml")
	}

	// Use the test config
	os.Rename("config.yaml.test", "config.yaml")
	defer os.Remove("config.yaml")

	// Test loading from YAML
	cfg := Load()
	if cfg.Server.Port != "9090" {
		t.Errorf("expected 9090, got %s", cfg.Server.Port)
	}
	if cfg.DB.Mongo.URI != "mongodb://test:27017" {
		t.Errorf("expected mongodb://test:27017, got %s", cfg.DB.Mongo.URI)
	}
	if cfg.ThirdPartyAPI.AlphaVantage.APIKey != "test-key" {
		t.Errorf("expected test-key, got %s", cfg.ThirdPartyAPI.AlphaVantage.APIKey)
	}

	// Test environment variable override
	os.Setenv("SERVER_PORT", "9999")
	os.Setenv("ALPHA_VANTAGE_API_KEY", "env-key")
	defer os.Unsetenv("SERVER_PORT")
	defer os.Unsetenv("ALPHA_VANTAGE_API_KEY")

	cfg = Load()
	if cfg.Server.Port != "9999" {
		t.Errorf("expected 9999 (env override), got %s", cfg.Server.Port)
	}
	if cfg.ThirdPartyAPI.AlphaVantage.APIKey != "env-key" {
		t.Errorf("expected env-key (env override), got %s", cfg.ThirdPartyAPI.AlphaVantage.APIKey)
	}
}
