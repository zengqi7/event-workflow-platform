package infra

import (
	"fmt"
	"os"
)

type Config struct {
	AppName       string
	Environment   string
	HTTPAddr      string
	MySQLDSN      string
	RedisAddr     string
	RedisPassword string
	RedisDB       int
}

func LoadConfig() Config {
	return Config{
		AppName:       getenv("APP_NAME", "event-workflow-platform"),
		Environment:   getenv("APP_ENV", "dev"),
		HTTPAddr:      getenv("HTTP_ADDR", "127.0.0.1:8080"),
		MySQLDSN:      getenv("MYSQL_DSN", ""),
		RedisAddr:     getenv("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword: getenv("REDIS_PASSWORD", ""),
		RedisDB:       getenvInt("REDIS_DB", 0),
	}
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func getenvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	var parsed int
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil {
		return fallback
	}
	return parsed
}
