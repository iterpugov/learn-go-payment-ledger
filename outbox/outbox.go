package outbox

import (
	"context"
	"encoding/json"
	"time"
)

// Event — строка outbox (контракт совместим с Debezium Outbox Event Router).
type Event struct {
	ID            string
	AggregateType string // напр. "Transfer"
	AggregateID   string
	Type          string // напр. "TransferPosted"
	Payload       json.RawMessage
	CreatedAt     time.Time
	PublishedAt   *time.Time // для трека A; Debezium игнорирует
}

// Publisher доставляет событие (лог / канал / Kafka — на выбор).
type Publisher interface {
	Publish(ctx context.Context, ev Event) error
}

// LogPublisher — заглушка: пишет событие в slog/stdout.
type LogPublisher struct{}

func NewLogPublisher() *LogPublisher {
	return &LogPublisher{}
}

func (p *LogPublisher) Publish(ctx context.Context, ev Event) error {
	panic("TODO: LogPublisher.Publish")
}
