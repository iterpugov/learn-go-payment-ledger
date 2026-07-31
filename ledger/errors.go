package ledger

import "errors"

var (
	ErrInsufficientFunds  = errors.New("insufficient funds")
	ErrCurrencyMismatch   = errors.New("currency mismatch")
	ErrSelfTransfer       = errors.New("self transfer not allowed")
	ErrInvalidAmount      = errors.New("amount must be positive")
	ErrAccountNotFound    = errors.New("account not found")
	ErrTransferNotFound   = errors.New("transfer not found")
	ErrIdempotencyConflict = errors.New("idempotency key reused with different params")
	ErrMissingIdempotencyKey = errors.New("idempotency key required")
)
