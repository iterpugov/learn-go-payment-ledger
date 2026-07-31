package store

import (
	"context"
	"sync"

	"iterpugov/go-payment-ledger/ledger"
)

// Memory — in-memory реализация для быстрых юнит-тестов.
// Не заменяет Postgres в Definition of Done.
type Memory struct {
	mu sync.Mutex
	// TODO: accounts, transfers, postings, outbox
}

func NewMemory() *Memory {
	return &Memory{}
}

func (m *Memory) CreateAccount(ctx context.Context, currency string) (ledger.Account, error) {
	panic("TODO: Memory.CreateAccount")
}

func (m *Memory) GetAccount(ctx context.Context, id string) (ledger.Account, error) {
	panic("TODO: Memory.GetAccount")
}

func (m *Memory) Balance(ctx context.Context, accountID string) (ledger.Money, error) {
	panic("TODO: Memory.Balance")
}

func (m *Memory) Transfer(ctx context.Context, req ledger.TransferRequest) (ledger.Transfer, error) {
	panic("TODO: Memory.Transfer")
}

func (m *Memory) GetTransfer(ctx context.Context, id string) (ledger.Transfer, error) {
	panic("TODO: Memory.GetTransfer")
}

func (m *Memory) GetTransferByIdempotencyKey(ctx context.Context, key string) (ledger.Transfer, error) {
	panic("TODO: Memory.GetTransferByIdempotencyKey")
}

func (m *Memory) Ping(ctx context.Context) error {
	return nil
}

var _ Repository = (*Memory)(nil)
