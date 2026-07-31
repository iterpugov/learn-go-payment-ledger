package ledger_test

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"testing"

	"iterpugov/go-payment-ledger/ledger"
	"iterpugov/go-payment-ledger/store"
)

func TestCreateAndGetAccount(t *testing.T) {
	ctx := context.Background()
	svc := newService()

	acc, err := svc.CreateAccount(ctx, "USD")
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if acc.ID == "" {
		t.Fatal("expected non-empty account id")
	}
	if acc.Currency != "USD" {
		t.Fatalf("currency: got %q, want USD", acc.Currency)
	}
	if acc.Balance != 0 {
		t.Fatalf("balance: got %d, want 0", acc.Balance)
	}

	got, err := svc.GetAccount(ctx, acc.ID)
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if got.ID != acc.ID || got.Currency != "USD" || got.Balance != 0 {
		t.Fatalf("GetAccount: %+v", got)
	}

	_, err = svc.GetAccount(ctx, "missing")
	if !errors.Is(err, ledger.ErrAccountNotFound) {
		t.Fatalf("GetAccount missing: got %v, want ErrAccountNotFound", err)
	}
}

func TestTransferHappyPath(t *testing.T) {
	ctx := context.Background()
	mem := store.NewMemory()
	svc := ledger.NewService(mem)

	from, to := mustCreatePair(t, svc, "USD")
	treasury := fund(t, mem, from.ID, 10_00, "USD")

	amount, err := ledger.NewAmount(3_50)
	if err != nil {
		t.Fatalf("NewAmount: %v", err)
	}
	tr, err := svc.Transfer(ctx, ledger.TransferRequest{
		IdempotencyKey: "hp-1",
		FromAccount:    from.ID,
		ToAccount:      to.ID,
		Amount:         amount,
		Currency:       "USD",
	})
	if err != nil {
		t.Fatalf("Transfer: %v", err)
	}
	if tr.Status != ledger.StatusPosted {
		t.Fatalf("status: got %q, want POSTED", tr.Status)
	}
	if tr.ID == "" || tr.Amount != amount || tr.Currency != "USD" {
		t.Fatalf("transfer fields: %+v", tr)
	}
	if tr.FromAccount != from.ID || tr.ToAccount != to.ID {
		t.Fatalf("transfer accounts: %+v", tr)
	}

	fromAcc := mustGet(t, svc, from.ID)
	toAcc := mustGet(t, svc, to.ID)
	if fromAcc.Balance != 10_00-amount {
		t.Fatalf("from balance: got %d, want %d", fromAcc.Balance, 10_00-amount)
	}
	if toAcc.Balance != amount {
		t.Fatalf("to balance: got %d, want %d", toAcc.Balance, amount)
	}

	// seed ±amount + перевод ±amount → сумма балансов всех счетов = 0
	assertBalancesSumZero(t, svc, from.ID, to.ID, treasury)

	got, err := svc.GetTransfer(ctx, tr.ID)
	if err != nil {
		t.Fatalf("GetTransfer: %v", err)
	}
	if got.ID != tr.ID {
		t.Fatalf("GetTransfer id: got %q, want %q", got.ID, tr.ID)
	}
}

func TestTransferInsufficientFunds(t *testing.T) {
	ctx := context.Background()
	mem := store.NewMemory()
	svc := ledger.NewService(mem)

	from, to := mustCreatePair(t, svc, "USD")
	treasury := fund(t, mem, from.ID, 100, "USD")

	_, err := svc.Transfer(ctx, ledger.TransferRequest{
		IdempotencyKey: "insuf-1",
		FromAccount:    from.ID,
		ToAccount:      to.ID,
		Amount:         101,
		Currency:       "USD",
	})
	if !errors.Is(err, ledger.ErrInsufficientFunds) {
		t.Fatalf("got %v, want ErrInsufficientFunds", err)
	}

	if mustGet(t, svc, from.ID).Balance != 100 {
		t.Fatalf("from balance changed on failure")
	}
	if mustGet(t, svc, to.ID).Balance != 0 {
		t.Fatalf("to balance changed on failure")
	}
	assertBalancesSumZero(t, svc, from.ID, to.ID, treasury)
}

func TestTransferSelfTransfer(t *testing.T) {
	ctx := context.Background()
	svc := newService()
	acc, err := svc.CreateAccount(ctx, "USD")
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.Transfer(ctx, ledger.TransferRequest{
		IdempotencyKey: "self-1",
		FromAccount:    acc.ID,
		ToAccount:      acc.ID,
		Amount:         1,
		Currency:       "USD",
	})
	if !errors.Is(err, ledger.ErrSelfTransfer) {
		t.Fatalf("got %v, want ErrSelfTransfer", err)
	}
}

func TestTransferCurrencyMismatch(t *testing.T) {
	ctx := context.Background()
	mem := store.NewMemory()
	svc := ledger.NewService(mem)

	from, err := svc.CreateAccount(ctx, "USD")
	if err != nil {
		t.Fatal(err)
	}
	to, err := svc.CreateAccount(ctx, "EUR")
	if err != nil {
		t.Fatal(err)
	}
	fund(t, mem, from.ID, 1000, "USD")

	_, err = svc.Transfer(ctx, ledger.TransferRequest{
		IdempotencyKey: "fx-1",
		FromAccount:    from.ID,
		ToAccount:      to.ID,
		Amount:         10,
		Currency:       "USD",
	})
	if !errors.Is(err, ledger.ErrCurrencyMismatch) {
		t.Fatalf("account currencies: got %v, want ErrCurrencyMismatch", err)
	}

	a, b := mustCreatePair(t, svc, "USD")
	fund(t, mem, a.ID, 1000, "USD")
	_, err = svc.Transfer(ctx, ledger.TransferRequest{
		IdempotencyKey: "fx-2",
		FromAccount:    a.ID,
		ToAccount:      b.ID,
		Amount:         10,
		Currency:       "EUR",
	})
	if !errors.Is(err, ledger.ErrCurrencyMismatch) {
		t.Fatalf("request currency: got %v, want ErrCurrencyMismatch", err)
	}
}

func TestTransferInvalidAmount(t *testing.T) {
	ctx := context.Background()
	svc := newService()
	from, to := mustCreatePair(t, svc, "USD")

	for _, amount := range []ledger.Money{0, -1} {
		_, err := svc.Transfer(ctx, ledger.TransferRequest{
			IdempotencyKey: "amt",
			FromAccount:    from.ID,
			ToAccount:      to.ID,
			Amount:         amount,
			Currency:       "USD",
		})
		if !errors.Is(err, ledger.ErrInvalidAmount) {
			t.Fatalf("amount=%d: got %v, want ErrInvalidAmount", amount, err)
		}
	}
}

func TestTransferAccountNotFound(t *testing.T) {
	ctx := context.Background()
	svc := newService()
	to, err := svc.CreateAccount(ctx, "USD")
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.Transfer(ctx, ledger.TransferRequest{
		IdempotencyKey: "nf-1",
		FromAccount:    "missing-from",
		ToAccount:      to.ID,
		Amount:         1,
		Currency:       "USD",
	})
	if !errors.Is(err, ledger.ErrAccountNotFound) {
		t.Fatalf("got %v, want ErrAccountNotFound", err)
	}
}

func TestTransferIdempotentReplay(t *testing.T) {
	ctx := context.Background()
	mem := store.NewMemory()
	svc := ledger.NewService(mem)

	from, to := mustCreatePair(t, svc, "USD")
	treasury := fund(t, mem, from.ID, 10_00, "USD")

	req := ledger.TransferRequest{
		IdempotencyKey: "idem-1",
		FromAccount:    from.ID,
		ToAccount:      to.ID,
		Amount:         2_00,
		Currency:       "USD",
	}

	first, err := svc.Transfer(ctx, req)
	if err != nil {
		t.Fatalf("first Transfer: %v", err)
	}
	second, err := svc.Transfer(ctx, req)
	if err != nil {
		t.Fatalf("second Transfer: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("replay created new transfer: %q vs %q", first.ID, second.ID)
	}

	if mustGet(t, svc, from.ID).Balance != 8_00 {
		t.Fatalf("from balance after replay: got %d, want 800", mustGet(t, svc, from.ID).Balance)
	}
	if mustGet(t, svc, to.ID).Balance != 2_00 {
		t.Fatalf("to balance after replay: got %d, want 200", mustGet(t, svc, to.ID).Balance)
	}
	assertBalancesSumZero(t, svc, from.ID, to.ID, treasury)
}

func TestTransferIdempotencyConflict(t *testing.T) {
	ctx := context.Background()
	mem := store.NewMemory()
	svc := ledger.NewService(mem)

	from, to := mustCreatePair(t, svc, "USD")
	fund(t, mem, from.ID, 10_00, "USD")

	_, err := svc.Transfer(ctx, ledger.TransferRequest{
		IdempotencyKey: "conflict-1",
		FromAccount:    from.ID,
		ToAccount:      to.ID,
		Amount:         1_00,
		Currency:       "USD",
	})
	if err != nil {
		t.Fatalf("first Transfer: %v", err)
	}

	_, err = svc.Transfer(ctx, ledger.TransferRequest{
		IdempotencyKey: "conflict-1",
		FromAccount:    from.ID,
		ToAccount:      to.ID,
		Amount:         2_00,
		Currency:       "USD",
	})
	if !errors.Is(err, ledger.ErrIdempotencyConflict) {
		t.Fatalf("got %v, want ErrIdempotencyConflict", err)
	}

	if mustGet(t, svc, from.ID).Balance != 9_00 || mustGet(t, svc, to.ID).Balance != 1_00 {
		t.Fatalf("balances after conflict: from=%d to=%d",
			mustGet(t, svc, from.ID).Balance, mustGet(t, svc, to.ID).Balance)
	}
}

// --- helpers ---

func newService() *ledger.Service {
	return ledger.NewService(store.NewMemory())
}

func mustCreatePair(t *testing.T, svc *ledger.Service, currency string) (from, to ledger.Account) {
	t.Helper()
	ctx := context.Background()
	var err error
	from, err = svc.CreateAccount(ctx, currency)
	if err != nil {
		t.Fatalf("CreateAccount from: %v", err)
	}
	to, err = svc.CreateAccount(ctx, currency)
	if err != nil {
		t.Fatalf("CreateAccount to: %v", err)
	}
	return from, to
}

func mustGet(t *testing.T, svc *ledger.Service, id string) ledger.Account {
	t.Helper()
	acc, err := svc.GetAccount(context.Background(), id)
	if err != nil {
		t.Fatalf("GetAccount(%s): %v", id, err)
	}
	return acc
}

// fund кладёт amount на accountID с компенсирующей проводкой на treasury,
// чтобы сумма всех проводок оставалась 0. Работает и с Memory, и с Postgres
// (FK на transfers: сначала InsertTransfer, потом две проводки).
func fund(t *testing.T, st ledger.Store, accountID string, amount ledger.Money, currency string) string {
	t.Helper()
	ctx := context.Background()
	treasury, err := st.CreateAccount(ctx, currency)
	if err != nil {
		t.Fatalf("CreateAccount treasury: %v", err)
	}
	err = st.WithinTx(ctx, func(q ledger.TxQueries) error {
		tr, err := q.InsertTransfer(ctx, ledger.TransferRequest{
			IdempotencyKey: "fund-" + newTestID(),
			FromAccount:    treasury.ID,
			ToAccount:      accountID,
			Amount:         amount,
			Currency:       currency,
		})
		if err != nil {
			return err
		}
		if err := q.InsertPosting(ctx, ledger.Posting{
			TransferID: tr.ID,
			AccountID:  treasury.ID,
			Amount:     -amount,
			Currency:   currency,
		}); err != nil {
			return err
		}
		return q.InsertPosting(ctx, ledger.Posting{
			TransferID: tr.ID,
			AccountID:  accountID,
			Amount:     amount,
			Currency:   currency,
		})
	})
	if err != nil {
		t.Fatalf("fund: %v", err)
	}
	return treasury.ID
}

func newTestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%x", b[:])
}

func assertBalancesSumZero(t *testing.T, svc *ledger.Service, ids ...string) {
	t.Helper()
	var sum ledger.Money
	for _, id := range ids {
		sum += mustGet(t, svc, id).Balance
	}
	if sum != 0 {
		t.Fatalf("sum of account balances: got %d, want 0", sum)
	}
}
