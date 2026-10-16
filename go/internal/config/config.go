package config

import (
	"os"
	"strconv"

	"github.com/redis/go-redis/v9"
)

type Config struct {
	ServiceName string
	Environment string
	LogLevel    string

	KafkaBootstrapServers []string
	RedisAddr             string
	ClickHouseAddr        string
	PostgresURL           string

	IngestGatewayPort int
	QueryAPIPort      int
}

func Load() *Config {
	return &Config{
		ServiceName: getEnv("SERVICE_NAME", "platform"),
		Environment: getEnv("ENVIRONMENT", "development"),
		LogLevel:    getEnv("LOG_LEVEL", "info"),

		KafkaBootstrapServers: []string{getEnv("KAFKA_BOOTSTRAP_SERVERS", "localhost:9092")},
		RedisAddr:             getEnv("REDIS_ADDR", "localhost:6379"),
		ClickHouseAddr:        getEnv("CLICKHOUSE_ADDR", "localhost:9000"),
		PostgresURL:           getEnv("POSTGRES_URL", "postgres://platform:platform@localhost:5432/platform?sslmode=disable"),

		IngestGatewayPort: getEnvInt("INGEST_GATEWAY_PORT", 8080),
		QueryAPIPort:      getEnvInt("QUERY_API_PORT", 8081),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

// RedisOptions reads REDIS_URL (redis:// or rediss:// with credentials) when
// set, else REDIS_ADDR with an optional REDIS_PASSWORD.
func (c *Config) RedisOptions() (*redis.Options, error) {
	if u := os.Getenv("REDIS_URL"); u != "" {
		return redis.ParseURL(u)
	}
	return &redis.Options{Addr: c.RedisAddr, Password: os.Getenv("REDIS_PASSWORD")}, nil
}
