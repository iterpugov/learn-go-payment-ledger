package store

import (
	"context"
	"database/sql"
	"errors"
	"iterpugov/go-payment-ledger/ledger"
)

// Postgres — основная реализация на БД (транзакции, unique, FOR UPDATE).
type Postgres struct {
	db *sql.DB
}

func NewPostgres(db *sql.DB) *Postgres {
	return &Postgres{db: db}
}

func (p *Postgres) CreateAccount(ctx context.Context, currency string) (ledger.Account, error) {
	row, err := p.db.QueryContext(ctx, "INSERT INTO accounts (currency) VALUES ($1) RETURNING id", currency)
	if err != nil {
		return ledger.Account{}, err
	}
	defer row.Close()
	if !row.Next() {
		return ledger.Account{}, errors.New("account not created")
	}
	var id string
	err = row.Scan(&id)
	if err != nil {
		return ledger.Account{}, err
	}
	return ledger.Account{
		ID:       id,
		Currency: currency,
		Balance:  0,
	}, nil
}

func (p *Postgres) GetAccount(ctx context.Context, id string) (ledger.Account, error) {
	row, err := p.db.QueryContext(ctx, "SELECT currency FROM accounts WHERE id = $1", id)
	if err != nil {
		return ledger.Account{}, err
	}
	defer row.Close()
	if !row.Next() {
		return ledger.Account{}, errors.New("account not found")
	}
	var account = ledger.Account{ID: id}
	err = row.Scan(&account.Currency)
	if err != nil {
		return ledger.Account{}, err
	}
	balance, err := p.Balance(ctx, id)
	if err != nil {
		return ledger.Account{}, err
	}
	account.Balance = balance

	return account, nil
}

func (p *Postgres) Balance(ctx context.Context, accountID string) (ledger.Money, error) {
	row, err := p.db.QueryContext(ctx, "SELECT SUM(amount) FROM postings WHERE account_id = $1", accountID)
	if err != nil {
		return 0, err
	}
	defer row.Close()
	if !row.Next() {
		return 0, errors.New("no balance found")
	}

	var balance ledger.Money
	err = row.Scan(&balance)
	if err != nil {
		return 0, err
	}
	return balance, nil
}

func (p *Postgres) Transfer(ctx context.Context, req ledger.TransferRequest) (ledger.Transfer, error) {
	panic("TODO: Postgres.Transfer")
}

func (p *Postgres) GetTransfer(ctx context.Context, id string) (ledger.Transfer, error) {
	panic("TODO: Postgres.GetTransfer")
}

func (p *Postgres) GetTransferByIdempotencyKey(ctx context.Context, key string) (ledger.Transfer, error) {
	panic("TODO: Postgres.GetTransferByIdempotencyKey")
}

func (p *Postgres) Ping(ctx context.Context) error {
	return p.db.PingContext(ctx)
}

var _ Repository = (*Postgres)(nil)
