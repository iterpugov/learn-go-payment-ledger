package store

import (
	"context"

	"iterpugov/go-payment-ledger/ledger"
)

// Repository — слой хранения за интерфейсом.
// Транзакции (проводки + outbox) должны коммититься атомарно.
type Repository interface {
	CreateAccount(ctx context.Context, currency string) (ledger.Account, error)
	GetAccount(ctx context.Context, id string) (ledger.Account, error)
	Balance(ctx context.Context, accountID string) (ledger.Money, error)

	// Transfer атомарно: идемпотентность, 2 проводки, outbox-событие, статус.
	Transfer(ctx context.Context, req ledger.TransferRequest) (ledger.Transfer, error)
	GetTransfer(ctx context.Context, id string) (ledger.Transfer, error)
	GetTransferByIdempotencyKey(ctx context.Context, key string) (ledger.Transfer, error)

	Ping(ctx context.Context) error
}
