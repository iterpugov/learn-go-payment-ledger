package store

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	"iterpugov/go-payment-ledger/ledger"
)

// Memory — in-memory реализация для быстрых юнит-тестов.
// Не заменяет Postgres в Definition of Done.
type Memory struct {
	mu        sync.Mutex
	accounts  map[string]ledger.Account // Balance не хранится — считается из postings
	transfers map[string]ledger.Transfer
	byIdem    map[string]string // idempotency_key → transfer id
	postings  []ledger.Posting
	outbox    []memOutbox
}

type memOutbox struct {
	AggregateID string
	Payload     []byte
}

func NewMemory() *Memory {
	return &Memory{
		accounts:  make(map[string]ledger.Account),
		transfers: make(map[string]ledger.Transfer),
		byIdem:    make(map[string]string),
	}
}

func (m *Memory) CreateAccount(ctx context.Context, currency string) (ledger.Account, error) {
	if err := ctx.Err(); err != nil {
		return ledger.Account{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	acc := ledger.Account{
		ID:       newID(),
		Currency: currency,
		Balance:  0,
	}
	m.accounts[acc.ID] = acc
	return acc, nil
}

func (m *Memory) GetAccount(ctx context.Context, id string) (ledger.Account, error) {
	if err := ctx.Err(); err != nil {
		return ledger.Account{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	acc, ok := m.accounts[id]
	if !ok {
		return ledger.Account{}, ledger.ErrAccountNotFound
	}
	acc.Balance = m.balanceLocked(id)
	return acc, nil
}

func (m *Memory) Balance(ctx context.Context, accountID string) (ledger.Money, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.balanceLocked(accountID), nil
}

func (m *Memory) GetTransfer(ctx context.Context, id string) (ledger.Transfer, error) {
	if err := ctx.Err(); err != nil {
		return ledger.Transfer{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	t, ok := m.transfers[id]
	if !ok {
		return ledger.Transfer{}, ledger.ErrTransferNotFound
	}
	return t, nil
}

// WithinTx — для in-memory «транзакции» нет; под mutex вызови fn с memQueries
// (реализует ledger.TxQueries поверх карт). Для happy-path юнит-тестов хватит:
// нет отката, но и конкурентности за пределами mutex тоже нет.
func (m *Memory) WithinTx(ctx context.Context, fn func(ledger.TxQueries) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return fn(&memQueries{m: m})
}

func (m *Memory) Ping(ctx context.Context) error {
	return ctx.Err()
}

func (m *Memory) balanceLocked(accountID string) ledger.Money {
	var sum ledger.Money
	for _, p := range m.postings {
		if p.AccountID == accountID {
			sum += p.Amount
		}
	}
	return sum
}

// memQueries — реализация ledger.TxQueries поверх карт Memory.
// Вызывается только под m.mu (см. WithinTx).
type memQueries struct {
	m *Memory
}

func (q *memQueries) LockAccount(ctx context.Context, id string) (ledger.Account, error) {
	if err := ctx.Err(); err != nil {
		return ledger.Account{}, err
	}
	acc, ok := q.m.accounts[id]
	if !ok {
		return ledger.Account{}, ledger.ErrAccountNotFound
	}
	return acc, nil
}

func (q *memQueries) BalanceTx(ctx context.Context, accountID string) (ledger.Money, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return q.m.balanceLocked(accountID), nil
}

func (q *memQueries) GetTransferByIdempotencyKey(ctx context.Context, key string) (ledger.Transfer, error) {
	if err := ctx.Err(); err != nil {
		return ledger.Transfer{}, err
	}
	id, ok := q.m.byIdem[key]
	if !ok {
		return ledger.Transfer{}, ledger.ErrTransferNotFound
	}
	return q.m.transfers[id], nil
}

func (q *memQueries) InsertTransfer(ctx context.Context, req ledger.TransferRequest) (ledger.Transfer, error) {
	if err := ctx.Err(); err != nil {
		return ledger.Transfer{}, err
	}
	if _, exists := q.m.byIdem[req.IdempotencyKey]; exists {
		return ledger.Transfer{}, ledger.ErrDuplicateIdempotencyKey
	}
	t := ledger.Transfer{
		ID:             newID(),
		IdempotencyKey: req.IdempotencyKey,
		FromAccount:    req.FromAccount,
		ToAccount:      req.ToAccount,
		Amount:         req.Amount,
		Currency:       req.Currency,
		Status:         ledger.StatusPosted,
		CreatedAt:      time.Now().UTC(),
	}
	q.m.transfers[t.ID] = t
	q.m.byIdem[t.IdempotencyKey] = t.ID
	return t, nil
}

func (q *memQueries) InsertPosting(ctx context.Context, p ledger.Posting) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.ID = newID()
	p.CreatedAt = time.Now().UTC()
	q.m.postings = append(q.m.postings, p)
	return nil
}

func (q *memQueries) InsertOutbox(ctx context.Context, aggregateID string, payload []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	q.m.outbox = append(q.m.outbox, memOutbox{
		AggregateID: aggregateID,
		Payload:     payload,
	})
	return nil
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

var (
	_ Repository       = (*Memory)(nil)
	_ ledger.Store     = (*Memory)(nil)
	_ ledger.TxQueries = (*memQueries)(nil)
)
