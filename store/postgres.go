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
	row, err := p.db.QueryContext(ctx, "SELECT COALESCE(SUM(amount), 0) FROM postings WHERE account_id = $1", accountID)
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

func (p *Postgres) GetTransfer(ctx context.Context, id string) (ledger.Transfer, error) {
	row, err := p.db.QueryContext(ctx, "SELECT idempotency_key, from_account, to_account, amount, currency, status, created_at FROM transfers WHERE id = $1", id)
	if err != nil {
		return ledger.Transfer{}, err
	}
	defer row.Close()
	if !row.Next() {
		return ledger.Transfer{}, errors.New("transfer not found")
	}
	var transfer = ledger.Transfer{ID: id}
	err = row.Scan(&transfer.IdempotencyKey, &transfer.FromAccount, &transfer.ToAccount, &transfer.Amount,
		&transfer.Currency, &transfer.Status, &transfer.CreatedAt)
	if err != nil {
		return ledger.Transfer{}, err
	}
	return transfer, nil
}

func (p *Postgres) Ping(ctx context.Context) error {
	return p.db.PingContext(ctx)
}

// WithinTx открывает транзакцию и отдаёт домену tx-скоупные запросы. fn вернула
// nil → COMMIT, иначе (или паника) → ROLLBACK через defer.
func (p *Postgres) WithinTx(ctx context.Context, fn func(ledger.TxQueries) error) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // no-op после успешного Commit

	if err := fn(&pgQueries{tx: tx}); err != nil {
		return err
	}
	return tx.Commit()
}

// pgQueries — реализация ledger.TxQueries поверх одной *sql.Tx. Каждый метод —
// один SQL, никакой бизнес-логики (она в ledger.Service.Transfer).
type pgQueries struct {
	tx *sql.Tx
}

func (q *pgQueries) LockAccount(ctx context.Context, id string) (ledger.Account, error) {
	acc := ledger.Account{ID: id}
	err := q.tx.QueryRowContext(ctx,
		"SELECT currency FROM accounts WHERE id = $1 FOR UPDATE", id,
	).Scan(&acc.Currency)
	if errors.Is(err, sql.ErrNoRows) {
		return ledger.Account{}, ledger.ErrAccountNotFound
	}
	if err != nil {
		return ledger.Account{}, err
	}
	return acc, nil // Balance не нужен домену здесь — он берёт BalanceTx отдельно
}

func (q *pgQueries) BalanceTx(ctx context.Context, accountID string) (ledger.Money, error) {
	var balance ledger.Money
	err := q.tx.QueryRowContext(ctx,
		"SELECT COALESCE(SUM(amount), 0) FROM postings WHERE account_id = $1", accountID,
	).Scan(&balance)
	if err != nil {
		return 0, err
	}
	return balance, nil
}

func (q *pgQueries) GetTransferByIdempotencyKey(ctx context.Context, key string) (ledger.Transfer, error) {
	t := ledger.Transfer{IdempotencyKey: key}
	err := q.tx.QueryRowContext(ctx,
		`SELECT id, from_account, to_account, amount, currency, status, created_at
		   FROM transfers WHERE idempotency_key = $1`, key,
	).Scan(&t.ID, &t.FromAccount, &t.ToAccount, &t.Amount, &t.Currency, &t.Status, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ledger.Transfer{}, ledger.ErrTransferNotFound
	}
	if err != nil {
		return ledger.Transfer{}, err
	}
	return t, nil
}

func (q *pgQueries) InsertTransfer(ctx context.Context, req ledger.TransferRequest) (ledger.Transfer, error) {
	transfer := ledger.Transfer{
		IdempotencyKey: req.IdempotencyKey,
		FromAccount:    req.FromAccount,
		ToAccount:      req.ToAccount,
		Amount:         req.Amount,
		Currency:       req.Currency,
		Status:         ledger.StatusPosted,
	}
	err := q.tx.QueryRowContext(ctx,
		`INSERT INTO transfers (idempotency_key, from_account, to_account, amount, currency, status)
		   VALUES ($1,$2,$3,$4,$5,'POSTED') RETURNING id, created_at`,
		req.IdempotencyKey, req.FromAccount, req.ToAccount, req.Amount, req.Currency,
	).Scan(&transfer.ID, &transfer.CreatedAt)
	if err != nil {
		// SQLState() есть у *pq.Error и *pgconn.PgError — без импорта драйвера.
		var pgErr interface{ SQLState() string }
		if errors.As(err, &pgErr) && pgErr.SQLState() == "23505" {
			return ledger.Transfer{}, ledger.ErrDuplicateIdempotencyKey
		}
		return ledger.Transfer{}, err
	}
	return transfer, nil
}

func (q *pgQueries) InsertPosting(ctx context.Context, p ledger.Posting) error {
	_, err := q.tx.ExecContext(ctx, "INSERT INTO postings (transfer_id, account_id, amount, currency) VALUES ($1,$2,$3,$4)",
		p.TransferID, p.AccountID, p.Amount, p.Currency,
	)

	return err
}

func (q *pgQueries) InsertOutbox(ctx context.Context, aggregateID string, payload []byte) error {
	_, err := q.tx.ExecContext(ctx, "INSERT INTO outbox (aggregatetype, aggregateid, type, payload) VALUES ('Transfer', $1, 'TransferPosted', $2)", aggregateID, payload)
	return err
}

var (
	_ Repository       = (*Postgres)(nil)
	_ ledger.Store     = (*Postgres)(nil)
	_ ledger.TxQueries = (*pgQueries)(nil)
)
