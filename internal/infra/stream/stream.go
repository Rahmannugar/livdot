// Package stream transports outbox events over Redis Streams. PostgreSQL stays
// the source of truth; the stream is at-least-once delivery.
package stream

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Rahmannugar/livdot/internal/infra/outbox"
	"github.com/redis/go-redis/v9"
)

// how long a pending entry waits before another consumer may reclaim it.
const minIdle = time.Minute

// fixed transport coordinates; not deploy-time configuration.
const (
	DefaultName  = "livdot:jobs"
	DefaultGroup = "livdot-workers"
)

// Bridge publishes outbox events to a Redis stream. It satisfies the outbox
// handler so the relay can hand events straight to the transport.
type Bridge struct {
	client *redis.Client
	stream string
}

func NewBridge(client *redis.Client, streamName string) (*Bridge, error) {
	if client == nil {
		return nil, fmt.Errorf("stream redis client is required")
	}
	if streamName == "" {
		return nil, fmt.Errorf("stream name is required")
	}
	return &Bridge{client: client, stream: streamName}, nil
}

func (bridge *Bridge) Handle(ctx context.Context, event outbox.Event) error {
	err := bridge.client.XAdd(ctx, &redis.XAddArgs{
		Stream: bridge.stream,
		Values: map[string]any{
			"id":              event.ID,
			"aggregate_type":  event.AggregateType,
			"aggregate_id":    event.AggregateID,
			"type":            event.Type,
			"payload":         string(event.Payload),
			"idempotency_key": event.IdempotencyKey,
		},
	}).Err()
	if err != nil {
		return fmt.Errorf("publish stream event: %w", err)
	}
	return nil
}

// Handler processes one transported event.
type Handler interface {
	Handle(ctx context.Context, event outbox.Event) error
}

// Consumer reads a stream through a consumer group. Entries are acknowledged
// only after the handler succeeds, so a crash reprocesses them.
type Consumer struct {
	client *redis.Client
	stream string
	group  string
	name   string
}

func NewConsumer(client *redis.Client, streamName, group, name string) (*Consumer, error) {
	if client == nil {
		return nil, fmt.Errorf("stream redis client is required")
	}
	if streamName == "" || group == "" || name == "" {
		return nil, fmt.Errorf("stream name, group, and consumer name are required")
	}
	return &Consumer{client: client, stream: streamName, group: group, name: name}, nil
}

// EnsureGroup creates the consumer group before any events are published, so
// early events are not missed.
func (consumer *Consumer) EnsureGroup(ctx context.Context) error {
	err := consumer.client.XGroupCreateMkStream(ctx, consumer.stream, consumer.group, "0").Err()
	if err != nil && !errors.Is(err, redis.Nil) && !isBusyGroup(err) {
		return fmt.Errorf("create consumer group: %w", err)
	}
	return nil
}

// Consume reclaims stale pending entries, then reads new ones. A nil result
// means the stream was empty for this cycle.
func (consumer *Consumer) Consume(
	ctx context.Context,
	count int64,
	handler Handler,
	block time.Duration,
) (int, error) {
	reclaimed, err := consumer.claimStale(ctx, count, handler)
	if err != nil {
		return reclaimed, err
	}
	streams, err := consumer.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    consumer.group,
		Consumer: consumer.name,
		Streams:  []string{consumer.stream, ">"},
		Count:    count,
		Block:    block,
	}).Result()
	if errors.Is(err, redis.Nil) {
		return reclaimed, nil
	}
	if err != nil {
		return reclaimed, fmt.Errorf("read stream: %w", err)
	}
	fresh, err := consumer.dispatch(ctx, flatten(streams), handler)
	return reclaimed + fresh, err
}

func (consumer *Consumer) claimStale(ctx context.Context, count int64, handler Handler) (int, error) {
	messages, _, err := consumer.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream:   consumer.stream,
		Group:    consumer.group,
		Consumer: consumer.name,
		MinIdle:  minIdle,
		Start:    "0-0",
		Count:    count,
	}).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return 0, fmt.Errorf("auto claim stream: %w", err)
	}
	return consumer.dispatch(ctx, messages, handler)
}

func (consumer *Consumer) dispatch(ctx context.Context, messages []redis.XMessage, handler Handler) (int, error) {
	processed := 0
	for _, message := range messages {
		if err := handler.Handle(ctx, decode(message)); err != nil {
			// leave unacknowledged so the entry is reclaimed and retried.
			continue
		}
		if err := consumer.client.XAck(ctx, consumer.stream, consumer.group, message.ID).Err(); err != nil {
			return processed, fmt.Errorf("ack stream entry: %w", err)
		}
		processed++
	}
	return processed, nil
}

func decode(message redis.XMessage) outbox.Event {
	return outbox.Event{
		ID:             stringValue(message.Values["id"]),
		AggregateType:  stringValue(message.Values["aggregate_type"]),
		AggregateID:    stringValue(message.Values["aggregate_id"]),
		Type:           stringValue(message.Values["type"]),
		Payload:        []byte(stringValue(message.Values["payload"])),
		IdempotencyKey: stringValue(message.Values["idempotency_key"]),
	}
}

func flatten(streams []redis.XStream) []redis.XMessage {
	var messages []redis.XMessage
	for _, stream := range streams {
		messages = append(messages, stream.Messages...)
	}
	return messages
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprintf("%v", value)
}

func isBusyGroup(err error) bool {
	return err != nil && len(err.Error()) >= 9 && err.Error()[:9] == "BUSYGROUP"
}
