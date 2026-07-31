package ledger

import (
	"context"
	"encoding/json"
	"errors"
)

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
	return s.store.GetAccount(ctx, id)
}

func (s *Service) GetTransfer(ctx context.Context, id string) (Transfer, error) {
	return s.store.GetTransfer(ctx, id)
}

// Transfer проводит перевод A→B одной локальной транзакцией. Вся доменная логика
// (правила, достаточность средств, знак проводок, идемпотентность) — здесь;
// store внутри WithinTx лишь читает под локом и пишет строки.
func (s *Service) Transfer(ctx context.Context, req TransferRequest) (Transfer, error) {
	// без-БД правила — до открытия транзакции
	if req.FromAccount == req.ToAccount {
		return Transfer{}, ErrSelfTransfer
	}
	if !req.Amount.IsPositive() {
		return Transfer{}, ErrInvalidAmount
	}

	var result Transfer
	err := s.store.WithinTx(ctx, func(q TxQueries) error {
		// 1. идемпотентность: ключ уже проведён? (fast-path; гарантию даёт unique-индекс ниже)
		switch existing, err := q.GetTransferByIdempotencyKey(ctx, req.IdempotencyKey); {
		case err == nil:
			if !sameTransfer(existing, req) {
				return ErrIdempotencyConflict
			}
			result = existing
			return nil
		case !errors.Is(err, ErrTransferNotFound):
			return err
		}

		// 2. оба счёта под блокировкой строки (фикс. порядок по id → без deadlock)
		from, to, err := lockPair(ctx, q, req.FromAccount, req.ToAccount)
		if err != nil {
			return err
		}

		// 3. доменные правила, требующие данных под локом
		if from.Currency != to.Currency || from.Currency != req.Currency {
			return ErrCurrencyMismatch
		}
		balance, err := q.BalanceTx(ctx, from.ID)
		if err != nil {
			return err
		}
		if balance < req.Amount {
			return ErrInsufficientFunds
		}

		// 4. запись: заголовок + 2 проводки + outbox — всё в этой же tx
		transfer, err := q.InsertTransfer(ctx, req)
		if errors.Is(err, ErrDuplicateIdempotencyKey) {
			// конкурентная вставка того же ключа выиграла гонку — перечитать и вернуть её
			existing, reErr := q.GetTransferByIdempotencyKey(ctx, req.IdempotencyKey)
			if reErr != nil {
				return reErr
			}
			if !sameTransfer(existing, req) {
				return ErrIdempotencyConflict
			}
			result = existing
			return nil
		}
		if err != nil {
			return err
		}

		if err := q.InsertPosting(ctx, Posting{TransferID: transfer.ID, AccountID: from.ID, Amount: -req.Amount, Currency: req.Currency}); err != nil {
			return err
		}
		if err := q.InsertPosting(ctx, Posting{TransferID: transfer.ID, AccountID: to.ID, Amount: req.Amount, Currency: req.Currency}); err != nil {
			return err
		}

		payload, err := json.Marshal(transfer)
		if err != nil {
			return err
		}
		if err := q.InsertOutbox(ctx, transfer.ID, payload); err != nil {
			return err
		}

		result = transfer
		return nil
	})
	return result, err
}

// sameTransfer — «та же операция?»: сверка семантики запроса с уже проведённым
// переводом по ключу. Разошлось → ErrIdempotencyConflict (в API 409).
func sameTransfer(t Transfer, req TransferRequest) bool {
	return t.FromAccount == req.FromAccount &&
		t.ToAccount == req.ToAccount &&
		t.Amount == req.Amount &&
		t.Currency == req.Currency
}

// lockPair блокирует оба счёта в детерминированном порядке по id, чтобы встречные
// переводы A→B и B→A не поймали deadlock. Возвращает счета как from/to.
func lockPair(ctx context.Context, q TxQueries, fromID, toID string) (from, to Account, err error) {
	if fromID < toID {
		if from, err = q.LockAccount(ctx, fromID); err != nil {
			return
		}
		to, err = q.LockAccount(ctx, toID)
		return
	}
	if to, err = q.LockAccount(ctx, toID); err != nil {
		return
	}
	from, err = q.LockAccount(ctx, fromID)
	return
}
