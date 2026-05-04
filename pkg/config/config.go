package config

import (
	"os"

	"github.com/goccy/go-yaml"
)

type Config struct {
	Server struct {
		Port string `yaml:"port"`
	} `yaml:"server"`
	DB struct {
		Mongo struct {
			URI string `yaml:"uri"`
		} `yaml:"mongo"`
	} `yaml:"db"`
	ThirdPartyAPI struct {
		AlphaVantage struct {
			APIKey string `yaml:"api_key"`
		} `yaml:"alpha_vantage"`
	} `yaml:"third_party_api"`
}

func Load() *Config {
	cfg := &Config{}
	cfg.Server.Port = "8080"

	if data, err := os.ReadFile("config.yaml"); err == nil {
		_ = yaml.Unmarshal(data, cfg)
	}

	// Environment variable overrides
	if port := os.Getenv("SERVER_PORT"); port != "" {
		cfg.Server.Port = port
	}
	if uri := os.Getenv("MONGO_URI"); uri != "" {
		cfg.DB.Mongo.URI = uri
	}
	if key := os.Getenv("ALPHA_VANTAGE_API_KEY"); key != "" {
		cfg.ThirdPartyAPI.AlphaVantage.APIKey = key
	}

	return cfg
}
