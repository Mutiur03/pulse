package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	for _, k := range []string{"CLICKHOUSE_ADDR", "CLICKHOUSE_DB", "CLICKHOUSE_USER", "S3_BUCKET", "S3_REGION",
		"S3_ARTIFACT_BUCKET", "S3_ENDPOINT", "KAFKA_TLS", "CLICKHOUSE_SECURE"} {
		t.Setenv(k, "")
	}
	c := Load()
	if c.ClickHouse.Addr != "localhost:9000" || c.ClickHouse.Database != "default" || c.ClickHouse.Username != "default" || c.ClickHouse.Secure {
		t.Fatalf("clickhouse %+v", c.ClickHouse)
	}
	if c.S3.Bucket != "pulse-replays" || c.S3.Region != "us-east-1" || c.S3.Endpoint != "" || c.ArtifactBucket != "pulse-artifacts" {
		t.Fatalf("s3 %+v %q", c.S3, c.ArtifactBucket)
	}
	if c.Kafka.TLS || c.QueryAPIPort != 8081 || c.IngestGatewayPort != 8080 {
		t.Fatalf("%+v", c)
	}
}

func TestLoadFromEnv(t *testing.T) {
	env := map[string]string{
		"CLICKHOUSE_ADDR": "ch:9440", "CLICKHOUSE_DB": "pulse", "CLICKHOUSE_USER": "u", "CLICKHOUSE_PASSWORD": "p",
		"CLICKHOUSE_SECURE": "true", "KAFKA_TLS": "true", "KAFKA_SASL_MECHANISM": "PLAIN", "KAFKA_SASL_USERNAME": "k",
		"KAFKA_SASL_PASSWORD": "kp", "S3_ENDPOINT": "s3:9000", "S3_ACCESS_KEY": "a", "S3_SECRET_KEY": "s",
		"S3_BUCKET": "b", "S3_REGION": "eu", "S3_USE_SSL": "true", "S3_ARTIFACT_BUCKET": "art",
		"SMTP_ADDR": "smtp:25", "SMTP_FROM": "f", "SMTP_USER": "su", "SMTP_PASSWORD": "sp",
		"APP_URL": "https://app", "OUTBOUND_ALLOWLIST": "10.0.0.0/8", "QUERY_API_PORT": "9000",
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
	c := Load()
	if o := c.ClickHouse; o.Addr != "ch:9440" || o.Database != "pulse" || o.Username != "u" || o.Password != "p" || !o.Secure || c.ClickHouseAddr != "ch:9440" {
		t.Fatalf("clickhouse %+v", o)
	}
	if k := c.Kafka; !k.TLS || k.SASLMechanism != "PLAIN" || k.SASLUsername != "k" || k.SASLPassword != "kp" {
		t.Fatalf("kafka %+v", k)
	}
	if s := c.S3; s.Endpoint != "s3:9000" || s.AccessKey != "a" || s.SecretKey != "s" || s.Bucket != "b" || s.Region != "eu" || !s.UseSSL || c.ArtifactBucket != "art" {
		t.Fatalf("s3 %+v", s)
	}
	if m := c.SMTP; m.Addr != "smtp:25" || m.From != "f" || m.User != "su" || m.Password != "sp" {
		t.Fatalf("smtp %+v", m)
	}
	if c.AppURL != "https://app" || c.OutboundAllowlist != "10.0.0.0/8" || c.QueryAPIPort != 9000 {
		t.Fatalf("%+v", c)
	}
}

func TestHelpers(t *testing.T) {
	t.Setenv("N", "-3")
	if Int("N", 7) != -3 || NonNegInt("N", 7) != 7 || PositiveInt("N", 7) != 7 {
		t.Fatal("negative")
	}
	t.Setenv("N", "0")
	if NonNegInt("N", 7) != 0 || PositiveInt("N", 7) != 7 {
		t.Fatal("zero")
	}
	t.Setenv("N", "x")
	if Int("N", 7) != 7 {
		t.Fatal("invalid")
	}
	t.Setenv("D", "15m")
	if Duration("D", time.Hour) != 15*time.Minute {
		t.Fatal("duration")
	}
	t.Setenv("D", "-1s")
	if Duration("D", time.Hour) != time.Hour {
		t.Fatal("negative duration")
	}
	t.Setenv("L", "a,b")
	if l := List("L"); len(l) != 2 || l[1] != "b" {
		t.Fatalf("list %v", l)
	}
	if String("UNSET_PULSE_VAR", "d") != "d" {
		t.Fatal("string")
	}
}

func TestRedisOptions(t *testing.T) {
	t.Setenv("REDIS_URL", "")
	t.Setenv("REDIS_PASSWORD", "pw")
	o, err := (&Config{RedisAddr: "r:6379"}).RedisOptions()
	if err != nil || o.Addr != "r:6379" || o.Password != "pw" {
		t.Fatalf("%+v %v", o, err)
	}
	t.Setenv("REDIS_URL", "rediss://:secret@cache:6380/0")
	o, err = (&Config{}).RedisOptions()
	if err != nil || o.Addr != "cache:6380" || o.Password != "secret" || o.TLSConfig == nil {
		t.Fatalf("%+v %v", o, err)
	}
}
