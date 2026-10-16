package kafka

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

// Security configures TLS and SASL for managed Kafka.
type Security struct {
	TLS bool
	// SASLMechanism is SCRAM-SHA-512 (the default), SCRAM-SHA-256 or PLAIN.
	// SASL is off when SASLUsername is empty.
	SASLMechanism string
	SASLUsername  string
	SASLPassword  string
}

func securityOpts(sec Security) ([]kgo.Opt, error) {
	var opts []kgo.Opt
	if sec.TLS {
		opts = append(opts, kgo.DialTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12}))
	}
	user, pass := sec.SASLUsername, sec.SASLPassword
	if user == "" {
		return opts, nil
	}
	switch mech := sec.SASLMechanism; mech {
	case "", "SCRAM-SHA-512":
		opts = append(opts, kgo.SASL(scram.Auth{User: user, Pass: pass}.AsSha512Mechanism()))
	case "SCRAM-SHA-256":
		opts = append(opts, kgo.SASL(scram.Auth{User: user, Pass: pass}.AsSha256Mechanism()))
	case "PLAIN":
		opts = append(opts, kgo.SASL(plain.Auth{User: user, Pass: pass}.AsMechanism()))
	default:
		return nil, fmt.Errorf("unsupported KAFKA_SASL_MECHANISM %q", mech)
	}
	return opts, nil
}

type Producer struct {
	client *kgo.Client
}

func NewProducer(brokers []string, security Security) (*Producer, error) {
	sec, err := securityOpts(security)
	if err != nil {
		return nil, err
	}
	client, err := kgo.NewClient(append([]kgo.Opt{
		kgo.SeedBrokers(brokers...),
		kgo.AllowAutoTopicCreation(),
	}, sec...)...)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka client: %w", err)
	}

	return &Producer{client: client}, nil
}

func (p *Producer) Produce(ctx context.Context, topic string, key, value []byte) error {
	record := &kgo.Record{
		Topic: topic,
		Key:   key,
		Value: value,
	}

	results := p.client.ProduceSync(ctx, record)
	if err := results.FirstErr(); err != nil {
		return fmt.Errorf("failed to produce message: %w", err)
	}

	return nil
}

// ProduceBatch writes the records concurrently and waits for all of them.
func (p *Producer) ProduceBatch(ctx context.Context, topic string, keys, values [][]byte) error {
	records := make([]*kgo.Record, len(values))
	for i := range values {
		records[i] = &kgo.Record{Topic: topic, Key: keys[i], Value: values[i]}
	}
	if err := p.client.ProduceSync(ctx, records...).FirstErr(); err != nil {
		return fmt.Errorf("failed to produce messages: %w", err)
	}
	return nil
}

func (p *Producer) Close() {
	p.client.Close()
}

type Consumer struct {
	client *kgo.Client
}

func NewConsumer(brokers []string, security Security, groupID string, topics []string) (*Consumer, error) {
	sec, err := securityOpts(security)
	if err != nil {
		return nil, err
	}
	client, err := kgo.NewClient(append([]kgo.Opt{
		kgo.SeedBrokers(brokers...),
		kgo.ConsumerGroup(groupID),
		kgo.ConsumeTopics(topics...),
		// Start from the earliest offset when the group has no committed
		// offset yet, so records produced before the first join are not lost.
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		// Topics are auto-created by the producer; refresh metadata often so
		// the consumer picks them up quickly after first produce.
		kgo.MetadataMaxAge(5 * time.Second),
		kgo.DisableAutoCommit(),
	}, sec...)...)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka consumer: %w", err)
	}

	return &Consumer{client: client}, nil
}

func (c *Consumer) Poll(ctx context.Context) ([]*kgo.Record, error) {
	fetches := c.client.PollFetches(ctx)
	if fetches.IsClientClosed() {
		return nil, fmt.Errorf("kafka client closed")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Per-partition errors (e.g. a topic that does not exist yet) are logged
	// but do not discard records fetched from healthy partitions.
	fetches.EachError(func(topic string, partition int32, err error) {
		log.Printf("Kafka fetch error on %s[%d]: %v", topic, partition, err)
	})

	var records []*kgo.Record
	fetches.EachRecord(func(r *kgo.Record) {
		records = append(records, r)
	})

	return records, nil
}

func (c *Consumer) Commit(ctx context.Context) error {
	return c.client.CommitUncommittedOffsets(ctx)
}

func (c *Consumer) Close() {
	c.client.Close()
}
