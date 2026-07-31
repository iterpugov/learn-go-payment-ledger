package ledger

import "context"

// Порты домена к persistence. Объявлены в ledger (у потребителя), а не в store:
// store импортирует ledger, обратный импорт дал бы цикл. Реализация — адаптеры
// в пакете store (Postgres/Memory, pgQueries). "Accept interfaces, return structs".

// Store — не-транзакционные операции, что ядру нужны от persistence.
// Транзакционный перевод оркестрируется доменом через TxRunner.WithinTx.
type Store interface {
	CreateAccount(ctx context.Context, currency string) (Account, error)
	GetAccount(ctx context.Context, id string) (Account, error)
	GetTransfer(ctx context.Context, id string) (Transfer, error)
	Ping(ctx context.Context) error

	TxRunner
}

// TxRunner запускает fn в одной транзакции: fn вернула nil → COMMIT, иначе
// ROLLBACK. Даёт домену границу транзакции, не протаскивая в него SQL.
type TxRunner interface {
	WithinTx(ctx context.Context, fn func(TxQueries) error) error
}

// TxQueries — то, что домену нужно ВНУТРИ одной tx. Реализуется store, привязан
// к конкретной *sql.Tx. Методы выражены в доменных типах (Account, Posting…).
type TxQueries interface {
	// GetTransferByIdempotencyKey → ErrTransferNotFound, если ключа ещё нет.
	GetTransferByIdempotencyKey(ctx context.Context, key string) (Transfer, error)
	// LockAccount читает счёт под блокировкой строки (SELECT … FOR UPDATE).
	LockAccount(ctx context.Context, id string) (Account, error)
	// BalanceTx — баланс в той же tx (видит незакоммиченные проводки).
	BalanceTx(ctx context.Context, accountID string) (Money, error)
	// InsertTransfer пишет заголовок (status=POSTED) и возвращает его с id.
	// На гонке по ключу → ErrDuplicateIdempotencyKey.
	InsertTransfer(ctx context.Context, req TransferRequest) (Transfer, error)
	InsertPosting(ctx context.Context, p Posting) error
	InsertOutbox(ctx context.Context, aggregateID string, payload []byte) error
}
