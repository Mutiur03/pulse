package kafka

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

type Producer struct {
	client *kgo.Client
}

func NewProducer(brokers []string) (*Producer, error) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.AllowAutoTopicCreation(),
	)
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

func (p *Producer) Close() {
	p.client.Close()
}

type Consumer struct {
	client *kgo.Client
}

func NewConsumer(brokers []string, groupID string, topics []string) (*Consumer, error) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ConsumerGroup(groupID),
		kgo.ConsumeTopics(topics...),
		// Start from the earliest offset when the group has no committed
		// offset yet, so records produced before the first join are not lost.
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		// Topics are auto-created by the producer; refresh metadata often so
		// the consumer picks them up quickly after first produce.
		kgo.MetadataMaxAge(5*time.Second),
		kgo.DisableAutoCommit(),
	)
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
