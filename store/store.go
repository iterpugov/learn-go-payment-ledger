package store

import (
	"context"

	"iterpugov/go-payment-ledger/ledger"
)

// Repository — слой хранения за интерфейсом. Не-транзакционные операции +
// граница транзакции (WithinTx). Проводки + outbox коммитятся атомарно внутри
// одной WithinTx, которой оркестрирует домен.
type Repository interface {
	CreateAccount(ctx context.Context, currency string) (ledger.Account, error)
	GetAccount(ctx context.Context, id string) (ledger.Account, error)
	Balance(ctx context.Context, accountID string) (ledger.Money, error)
	GetTransfer(ctx context.Context, id string) (ledger.Transfer, error)
	Ping(ctx context.Context) error

	// WithinTx запускает fn в одной транзакции (см. ledger.TxRunner).
	ledger.TxRunner
}
