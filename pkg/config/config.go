package config

import "os"

type Config struct {
	ServerAddress string
	MongoURI      string
}

func Load() *Config {
	addr := os.Getenv("SERVER_ADDRESS")
	if addr == "" {
		addr = ":8080"
	}
	return &Config{
		ServerAddress: addr,
		MongoURI:      os.Getenv("MONGO_URI"),
	}
}
