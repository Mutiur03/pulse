// Package config reads every service setting from the environment. Nothing
// outside this package calls os.Getenv; libraries take explicit options.
package config

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Mutiur03/pulse/internal/kafka"
	"github.com/Mutiur03/pulse/internal/mail"
	"github.com/Mutiur03/pulse/internal/replay"
	"github.com/Mutiur03/pulse/internal/store/ch"
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

	// ClickHouse is the connection for every service (CLICKHOUSE_ADDR,
	// CLICKHOUSE_DB, CLICKHOUSE_USER, CLICKHOUSE_PASSWORD, CLICKHOUSE_SECURE).
	ClickHouse ch.Options
	// Kafka is TLS and SASL for managed Kafka (KAFKA_TLS,
	// KAFKA_SASL_MECHANISM, KAFKA_SASL_USERNAME, KAFKA_SASL_PASSWORD).
	Kafka kafka.Security
	// S3 is object storage for replays (S3_ENDPOINT host:port, S3_ACCESS_KEY,
	// S3_SECRET_KEY, S3_BUCKET, S3_REGION, S3_USE_SSL). No endpoint disables it.
	S3 replay.S3Config
	// ArtifactBucket holds source maps on the same server (S3_ARTIFACT_BUCKET).
	ArtifactBucket string
	// SMTP sends email; without SMTP_ADDR emails are written to the log.
	SMTP mail.Config
	// AppURL is the web app's public base URL (APP_URL).
	AppURL string
	// OutboundAllowlist lets webhooks reach listed internal addresses
	// (OUTBOUND_ALLOWLIST, development only).
	OutboundAllowlist string
}

func Load() *Config {
	chAddr := String("CLICKHOUSE_ADDR", "localhost:9000")
	return &Config{
		ServiceName: String("SERVICE_NAME", "platform"),
		Environment: String("ENVIRONMENT", "development"),
		LogLevel:    String("LOG_LEVEL", "info"),

		KafkaBootstrapServers: []string{String("KAFKA_BOOTSTRAP_SERVERS", "localhost:9092")},
		RedisAddr:             String("REDIS_ADDR", "localhost:6379"),
		ClickHouseAddr:        chAddr,
		PostgresURL:           String("POSTGRES_URL", "postgres://platform:platform@localhost:5432/platform?sslmode=disable"),

		IngestGatewayPort: Int("INGEST_GATEWAY_PORT", 8080),
		QueryAPIPort:      Int("QUERY_API_PORT", 8081),

		ClickHouse: ch.Options{
			Addr:     chAddr,
			Database: String("CLICKHOUSE_DB", "default"),
			Username: String("CLICKHOUSE_USER", "default"),
			Password: os.Getenv("CLICKHOUSE_PASSWORD"),
			Secure:   os.Getenv("CLICKHOUSE_SECURE") == "true",
		},
		Kafka: kafka.Security{
			TLS:           os.Getenv("KAFKA_TLS") == "true",
			SASLMechanism: os.Getenv("KAFKA_SASL_MECHANISM"),
			SASLUsername:  os.Getenv("KAFKA_SASL_USERNAME"),
			SASLPassword:  os.Getenv("KAFKA_SASL_PASSWORD"),
		},
		S3: replay.S3Config{
			Endpoint:  os.Getenv("S3_ENDPOINT"),
			AccessKey: os.Getenv("S3_ACCESS_KEY"),
			SecretKey: os.Getenv("S3_SECRET_KEY"),
			Bucket:    String("S3_BUCKET", "pulse-replays"),
			Region:    String("S3_REGION", "us-east-1"),
			UseSSL:    os.Getenv("S3_USE_SSL") == "true",
		},
		ArtifactBucket: String("S3_ARTIFACT_BUCKET", "pulse-artifacts"),
		SMTP: mail.Config{
			Addr: os.Getenv("SMTP_ADDR"), From: os.Getenv("SMTP_FROM"),
			User: os.Getenv("SMTP_USER"), Password: os.Getenv("SMTP_PASSWORD"),
		},
		AppURL:            os.Getenv("APP_URL"),
		OutboundAllowlist: os.Getenv("OUTBOUND_ALLOWLIST"),
	}
}

// String returns the variable, or def when it is unset or empty.
func String(key, def string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return def
}

// Int returns the variable as an integer, or def when it is unset or not a
// number.
func Int(key string, def int) int {
	if value := os.Getenv(key); value != "" {
		if n, err := strconv.Atoi(value); err == nil {
			return n
		}
	}
	return def
}

// NonNegInt is Int that also falls back to def for negative values.
func NonNegInt(key string, def int) int {
	if n := Int(key, -1); n >= 0 {
		return n
	}
	return def
}

// PositiveInt is Int that also falls back to def for zero or less.
func PositiveInt(key string, def int) int {
	if n := Int(key, 0); n > 0 {
		return n
	}
	return def
}

// Duration parses a Go duration such as "15m", falling back to def when the
// variable is unset, invalid or not positive.
func Duration(key string, def time.Duration) time.Duration {
	if d, err := time.ParseDuration(os.Getenv(key)); err == nil && d > 0 {
		return d
	}
	return def
}

// List splits a comma-separated variable.
func List(key string) []string {
	return strings.Split(os.Getenv(key), ",")
}

// RedisOptions reads REDIS_URL (redis:// or rediss:// with credentials) when
// set, else REDIS_ADDR with an optional REDIS_PASSWORD.
func (c *Config) RedisOptions() (*redis.Options, error) {
	if u := os.Getenv("REDIS_URL"); u != "" {
		return redis.ParseURL(u)
	}
	return &redis.Options{Addr: c.RedisAddr, Password: os.Getenv("REDIS_PASSWORD")}, nil
}
