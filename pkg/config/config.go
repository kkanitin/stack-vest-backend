package config

import (
	"os"
	"reflect"
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

	// Environment variable overrides — env key = yaml path uppercased, dots
	// replaced by underscores (e.g. third_party_api.fmp.api_key →
	// THIRD_PARTY_API_FMP_API_KEY). New config fields are picked up
	// automatically, no code change needed.
	applyEnvOverrides(reflect.ValueOf(cfg).Elem(), "")

	return cfg
}

// applyEnvOverrides walks the config struct and, for every leaf field, applies
// the matching environment variable (if set and non-empty). The env key is the
// chain of yaml tags from the root to the field, joined with "_" and uppercased.
func applyEnvOverrides(v reflect.Value, prefix string) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		tag := strings.Split(t.Field(i).Tag.Get("yaml"), ",")[0]
		if tag == "" || tag == "-" {
			continue
		}

		key := strings.ToUpper(tag)
		if prefix != "" {
			key = prefix + "_" + key
		}

		field := v.Field(i)
		if field.Kind() == reflect.Struct {
			applyEnvOverrides(field, key)
			continue
		}

		if raw := os.Getenv(key); raw != "" {
			setField(field, raw)
		}
	}
}

// setField parses raw into field according to the field's kind. Unsupported
// kinds and unparseable values are ignored, leaving the field unchanged.
func setField(field reflect.Value, raw string) {
	switch field.Kind() {
	case reflect.String:
		field.SetString(raw)
	case reflect.Bool:
		field.SetBool(parseBool(raw))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			field.SetInt(n)
		}
	case reflect.Slice:
		if field.Type().Elem().Kind() == reflect.String {
			field.Set(reflect.ValueOf(strings.Split(raw, ",")))
		}
	}
}

func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}
