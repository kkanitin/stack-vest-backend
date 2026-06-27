package config

import (
	"os"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
)

type Config struct {
	Server struct {
		Port string `yaml:"port"`
	} `yaml:"server"`
	Log struct {
		Level  string `yaml:"level"`
		Format string `yaml:"format"`
	} `yaml:"log"`
	DB struct {
		Postgres struct {
			DSN string `yaml:"dsn"`
		} `yaml:"postgres"`
		Migrate struct {
			Enabled bool `yaml:"enabled"`
		} `yaml:"migrate"`
	} `yaml:"db"`
	Auth struct {
		Google struct {
			ClientID     string `yaml:"client_id"`
			ClientSecret string `yaml:"client_secret"`
			RedirectURL  string `yaml:"redirect_url"`
		} `yaml:"google"`
		JWT struct {
			Secret string `yaml:"secret"`
		} `yaml:"jwt"`
	} `yaml:"auth"`
	ThirdPartyAPI struct {
		FMP struct {
			APIKey string `yaml:"api_key"`
		} `yaml:"fmp"`
		Groq struct {
			APIKey string `yaml:"api_key"`
		} `yaml:"groq"`
	} `yaml:"third_party_api"`
	Redis struct {
		Addr     string `yaml:"addr"`
		Password string `yaml:"password"`
		DB       int    `yaml:"db"`
	} `yaml:"redis"`
	CORS struct {
		AllowOrigins []string `yaml:"allow_origins"`
	} `yaml:"cors"`
	Portfolio struct {
		MaxPerUser               int `yaml:"max_per_user"`
		MaxPositionsPerPortfolio int `yaml:"max_positions_per_portfolio"`
	} `yaml:"portfolio"`
}

func Load() *Config {
	cfg := &Config{}
	cfg.Server.Port = "8080"
	cfg.Log.Level = "info"
	cfg.Log.Format = "json"
	cfg.DB.Migrate.Enabled = true
	cfg.Redis.Addr = "localhost:6379"
	cfg.Portfolio.MaxPerUser = 10
	cfg.Portfolio.MaxPositionsPerPortfolio = 20

	if data, err := os.ReadFile("config.yaml"); err == nil {
		_ = yaml.Unmarshal(data, cfg)
	}

	// Environment variable overrides — name = config path uppercased with dots replaced by underscores
	if v := os.Getenv("SERVER_PORT"); v != "" {
		cfg.Server.Port = v
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.Log.Level = v
	}
	if v := os.Getenv("LOG_FORMAT"); v != "" {
		cfg.Log.Format = v
	}
	if v := os.Getenv("DB_POSTGRES_DSN"); v != "" {
		cfg.DB.Postgres.DSN = v
	}
	if v := os.Getenv("DB_MIGRATE_ENABLED"); v != "" {
		cfg.DB.Migrate.Enabled = parseBool(v)
	}
	if v := os.Getenv("AUTH_GOOGLE_CLIENT_ID"); v != "" {
		cfg.Auth.Google.ClientID = v
	}
	if v := os.Getenv("AUTH_GOOGLE_CLIENT_SECRET"); v != "" {
		cfg.Auth.Google.ClientSecret = v
	}
	if v := os.Getenv("AUTH_GOOGLE_REDIRECT_URL"); v != "" {
		cfg.Auth.Google.RedirectURL = v
	}
	if v := os.Getenv("AUTH_JWT_SECRET"); v != "" {
		cfg.Auth.JWT.Secret = v
	}
	if v := os.Getenv("THIRD_PARTY_API_FMP_API_KEY"); v != "" {
		cfg.ThirdPartyAPI.FMP.APIKey = v
	}
	if v := os.Getenv("THIRD_PARTY_API_GROQ_API_KEY"); v != "" {
		cfg.ThirdPartyAPI.Groq.APIKey = v
	}
	if v := os.Getenv("REDIS_ADDR"); v != "" {
		cfg.Redis.Addr = v
	}
	if v := os.Getenv("REDIS_PASSWORD"); v != "" {
		cfg.Redis.Password = v
	}
	if v := os.Getenv("REDIS_DB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Redis.DB = n
		}
	}
	if v := os.Getenv("CORS_ALLOW_ORIGINS"); v != "" {
		cfg.CORS.AllowOrigins = strings.Split(v, ",")
	}
	if v := os.Getenv("PORTFOLIO_MAX_PER_USER"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Portfolio.MaxPerUser = n
		}
	}
	if v := os.Getenv("PORTFOLIO_MAX_POSITIONS_PER_PORTFOLIO"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Portfolio.MaxPositionsPerPortfolio = n
		}
	}

	return cfg
}

func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}
