package ledger

import "time"

// Доменная модель переводов (данные). Поведение — в ledger.go (Service),
// контракты к persistence — в ports.go.

type TransferStatus string

// Перевод — одна локальная транзакция: либо всё коммитится как POSTED, либо
// строки нет вообще. PENDING (двухфазность), FAILED (персист отказа отдельной
// tx) и REVERSED (компенсация/сага) вне scope — их состояния тут не возникают.
const StatusPosted TransferStatus = "POSTED"

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
