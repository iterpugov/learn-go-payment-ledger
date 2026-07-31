package outbox

import (
	"context"
	"time"
)

// Claimer выбирает неопубликованные события (FOR UPDATE SKIP LOCKED в Postgres).
type Claimer interface {
	ClaimUnpublished(ctx context.Context, limit int) ([]Event, error)
	MarkPublished(ctx context.Context, ids []string, at time.Time) error
}

// Relay — application-level поллер (трек A).
type Relay struct {
	claimer   Claimer
	publisher Publisher
	interval  time.Duration
	batchSize int
}

func NewRelay(claimer Claimer, publisher Publisher, interval time.Duration, batchSize int) *Relay {
	return &Relay{
		claimer:   claimer,
		publisher: publisher,
		interval:  interval,
		batchSize: batchSize,
	}
}

// Run крутится до отмены ctx (graceful shutdown).
func (r *Relay) Run(ctx context.Context) error {
	panic("TODO: Relay.Run")
}
