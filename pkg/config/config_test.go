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
third_party_api:
  fmp:
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
	if cfg.ThirdPartyAPI.FMP.APIKey != "test-key" {
		t.Errorf("expected test-key, got %s", cfg.ThirdPartyAPI.FMP.APIKey)
	}

	// Test environment variable override
	os.Setenv("SERVER_PORT", "9999")
	os.Setenv("THIRD_PARTY_API_FMP_API_KEY", "env-key")
	defer os.Unsetenv("SERVER_PORT")
	defer os.Unsetenv("THIRD_PARTY_API_FMP_API_KEY")

	cfg = Load()
	if cfg.Server.Port != "9999" {
		t.Errorf("expected 9999 (env override), got %s", cfg.Server.Port)
	}
	if cfg.ThirdPartyAPI.FMP.APIKey != "env-key" {
		t.Errorf("expected env-key (env override), got %s", cfg.ThirdPartyAPI.FMP.APIKey)
	}
}

func TestEnvOverrideTypes(t *testing.T) {
	t.Setenv("REDIS_DB", "3")
	t.Setenv("DB_MIGRATE_ENABLED", "false")
	t.Setenv("CORS_ALLOW_ORIGINS", "a.com,b.com")

	cfg := Load()
	if cfg.Redis.DB != 3 {
		t.Errorf("expected REDIS_DB=3, got %d", cfg.Redis.DB)
	}
	if cfg.DB.Migrate.Enabled != false {
		t.Errorf("expected DB_MIGRATE_ENABLED=false, got %v", cfg.DB.Migrate.Enabled)
	}
	if len(cfg.CORS.AllowOrigins) != 2 || cfg.CORS.AllowOrigins[0] != "a.com" || cfg.CORS.AllowOrigins[1] != "b.com" {
		t.Errorf("expected [a.com b.com], got %v", cfg.CORS.AllowOrigins)
	}
}

func TestEnvOverrideInvalidIntKeepsDefault(t *testing.T) {
	t.Setenv("REDIS_DB", "notanint")

	cfg := Load()
	if cfg.Redis.DB != 0 {
		t.Errorf("expected default REDIS_DB=0 for invalid int, got %d", cfg.Redis.DB)
	}
}

func TestLoadPortfolioBenchmarks(t *testing.T) {
	// Run from an empty dir so a developer's config.yaml cannot leak in.
	t.Chdir(t.TempDir())

	t.Run("default", func(t *testing.T) {
		cfg := Load()
		want := []string{"SPY=S&P 500", "QQQ=Nasdaq 100", "VT=Total world"}
		if len(cfg.Portfolio.Benchmarks) != len(want) {
			t.Fatalf("expected %v, got %v", want, cfg.Portfolio.Benchmarks)
		}
		for i := range want {
			if cfg.Portfolio.Benchmarks[i] != want[i] {
				t.Fatalf("expected %v, got %v", want, cfg.Portfolio.Benchmarks)
			}
		}
	})

	t.Run("yaml replaces default", func(t *testing.T) {
		if err := os.WriteFile("config.yaml", []byte(yamlBenchmarks), 0o644); err != nil {
			t.Fatal(err)
		}
		defer os.Remove("config.yaml")
		cfg := Load()
		if len(cfg.Portfolio.Benchmarks) != 1 || cfg.Portfolio.Benchmarks[0] != "DIA=Dow" {
			t.Fatalf("expected [DIA=Dow], got %v", cfg.Portfolio.Benchmarks)
		}
	})

	t.Run("env override is comma split and untrimmed", func(t *testing.T) {
		t.Setenv("PORTFOLIO_BENCHMARKS", "SPY=S&P 500, QQQ=Nasdaq 100")
		cfg := Load()
		if len(cfg.Portfolio.Benchmarks) != 2 || cfg.Portfolio.Benchmarks[1] != " QQQ=Nasdaq 100" {
			t.Fatalf("unexpected env override result: %q", cfg.Portfolio.Benchmarks)
		}
	})
}

const yamlBenchmarks = "portfolio:\n  benchmarks:\n    - \"DIA=Dow\"\n"
