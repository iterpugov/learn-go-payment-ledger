package ledger

import (
	"context"
	"time"
)

type TransferStatus string

const (
	StatusPending  TransferStatus = "PENDING"
	StatusPosted   TransferStatus = "POSTED"
	StatusFailed   TransferStatus = "FAILED"
	StatusReversed TransferStatus = "REVERSED"
)

type Account struct {
	ID       string
	Currency string
	Balance  Money
}

type Posting struct {
	ID         string
	TransferID string
	AccountID  string
	Amount     Money // знак: списание < 0, зачисление > 0
	Currency   string
	CreatedAt  time.Time
}

type Transfer struct {
	ID             string
	IdempotencyKey string
	FromAccount    string
	ToAccount      string
	Amount         Money
	Currency       string
	Status         TransferStatus
	CreatedAt      time.Time
}

type TransferRequest struct {
	IdempotencyKey string
	FromAccount    string
	ToAccount      string
	Amount         Money
	Currency       string
}

// Store — то, что ядру нужно от persistence (реализует store.Repository).
type Store interface {
	CreateAccount(ctx context.Context, currency string) (Account, error)
	GetAccount(ctx context.Context, id string) (Account, error)
	Transfer(ctx context.Context, req TransferRequest) (Transfer, error)
	GetTransfer(ctx context.Context, id string) (Transfer, error)
	Ping(ctx context.Context) error
}

// Service — доменное ядро переводов.
type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) CreateAccount(ctx context.Context, currency string) (Account, error) {
	account, err := s.store.CreateAccount(ctx, currency)
	if err != nil {
		return Account{}, err
	}
	return account, nil
}

func (s *Service) GetAccount(ctx context.Context, id string) (Account, error) {
	panic("TODO: GetAccount")
}

func (s *Service) Transfer(ctx context.Context, req TransferRequest) (Transfer, error) {
	panic("TODO: Transfer")
}

func (s *Service) GetTransfer(ctx context.Context, id string) (Transfer, error) {
	panic("TODO: GetTransfer")
}
