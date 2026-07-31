package ledger_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"iterpugov/go-payment-ledger/ledger"
	"iterpugov/go-payment-ledger/store"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// openPostgres: TEST_DSN / DSN, иначе дефолт Makefile. Нет БД → Skip.
func openPostgres(t *testing.T) (*store.Postgres, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("TEST_DSN")
	if dsn == "" {
		dsn = os.Getenv("DSN")
	}
	if dsn == "" {
		dsn = "postgres://ledger:ledger@localhost:5432/ledger?sslmode=disable"
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)

	if err := db.PingContext(context.Background()); err != nil {
		t.Skipf("postgres unavailable (%v); set TEST_DSN or make up", err)
	}
	return store.NewPostgres(db), db
}

func TestPostgresTransferHappyPath(t *testing.T) {
	pg, db := openPostgres(t)
	svc := ledger.NewService(pg)
	ctx := context.Background()

	from, to := mustCreatePair(t, svc, "USD")
	treasury := fund(t, pg, from.ID, 10_00, "USD")

	amount, err := ledger.NewAmount(3_50)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := svc.Transfer(ctx, ledger.TransferRequest{
		IdempotencyKey: "pg-hp-" + newTestID(),
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

	if mustGet(t, svc, from.ID).Balance != 10_00-amount {
		t.Fatalf("from balance: got %d", mustGet(t, svc, from.ID).Balance)
	}
	if mustGet(t, svc, to.ID).Balance != amount {
		t.Fatalf("to balance: got %d", mustGet(t, svc, to.ID).Balance)
	}
	assertBalancesSumZero(t, svc, from.ID, to.ID, treasury)

	// живая БД: две проводки перевода, сумма = 0
	var n int
	var sum int64
	err = db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(amount), 0) FROM postings WHERE transfer_id = $1`,
		tr.ID,
	).Scan(&n, &sum)
	if err != nil {
		t.Fatalf("postings query: %v", err)
	}
	if n != 2 || sum != 0 {
		t.Fatalf("postings for transfer: count=%d sum=%d, want 2 and 0", n, sum)
	}
}

// Конкурентные списания с одного счёта не уводят баланс в минус (FOR UPDATE).
func TestPostgresConcurrentDebits(t *testing.T) {
	pg, _ := openPostgres(t)
	svc := ledger.NewService(pg)
	ctx := context.Background()

	const (
		startBalance = ledger.Money(100)
		debit        = ledger.Money(10)
		workers      = 20 // 20×10 > 100 → часть получит ErrInsufficientFunds
	)

	from, to := mustCreatePair(t, svc, "USD")
	treasury := fund(t, pg, from.ID, startBalance, "USD")

	var (
		wg       sync.WaitGroup
		okCount  atomic.Int64
		failFund atomic.Int64
		otherErr atomic.Int64
	)

	wg.Add(workers)
	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()
			_, err := svc.Transfer(ctx, ledger.TransferRequest{
				IdempotencyKey: fmt.Sprintf("pg-race-%s-%d", newTestID(), i),
				FromAccount:    from.ID,
				ToAccount:      to.ID,
				Amount:         debit,
				Currency:       "USD",
			})
			switch {
			case err == nil:
				okCount.Add(1)
			case errors.Is(err, ledger.ErrInsufficientFunds):
				failFund.Add(1)
			default:
				otherErr.Add(1)
				t.Errorf("worker %d: unexpected error: %v", i, err)
			}
		}()
	}
	wg.Wait()

	if otherErr.Load() > 0 {
		t.Fatalf("unexpected errors from workers")
	}

	wantOK := int64(startBalance / debit) // 10
	if okCount.Load() != wantOK {
		t.Fatalf("successful transfers: got %d, want %d (fails=%d)",
			okCount.Load(), wantOK, failFund.Load())
	}
	if failFund.Load() != int64(workers)-wantOK {
		t.Fatalf("insufficient-funds: got %d, want %d",
			failFund.Load(), int64(workers)-wantOK)
	}

	fromBal := mustGet(t, svc, from.ID).Balance
	toBal := mustGet(t, svc, to.ID).Balance
	if fromBal < 0 {
		t.Fatalf("from balance went negative: %d", fromBal)
	}
	if fromBal != 0 {
		t.Fatalf("from balance: got %d, want 0", fromBal)
	}
	if toBal != startBalance {
		t.Fatalf("to balance: got %d, want %d", toBal, startBalance)
	}
	assertBalancesSumZero(t, svc, from.ID, to.ID, treasury)
}

// Конкурентный replay одного Idempotency-Key → ровно одна операция / одна строка transfers.
func TestPostgresConcurrentIdempotentReplay(t *testing.T) {
	pg, db := openPostgres(t)
	svc := ledger.NewService(pg)
	ctx := context.Background()

	const (
		amount  = ledger.Money(2_00)
		workers = 20
	)

	from, to := mustCreatePair(t, svc, "USD")
	treasury := fund(t, pg, from.ID, 10_00, "USD")

	key := "pg-idem-" + newTestID()
	req := ledger.TransferRequest{
		IdempotencyKey: key,
		FromAccount:    from.ID,
		ToAccount:      to.ID,
		Amount:         amount,
		Currency:       "USD",
	}

	start := make(chan struct{})
	var (
		wg       sync.WaitGroup
		ids      = make([]string, workers)
		otherErr atomic.Int64
	)

	wg.Add(workers)
	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()
			<-start
			tr, err := svc.Transfer(ctx, req)
			if err != nil {
				otherErr.Add(1)
				t.Errorf("worker %d: %v", i, err)
				return
			}
			ids[i] = tr.ID
		}()
	}
	close(start)
	wg.Wait()

	if otherErr.Load() > 0 {
		t.Fatalf("unexpected errors from workers")
	}

	first := ids[0]
	if first == "" {
		t.Fatal("empty transfer id")
	}
	for i, id := range ids {
		if id != first {
			t.Fatalf("worker %d got transfer %q, want %q (duplicate created)", i, id, first)
		}
	}

	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM transfers WHERE idempotency_key = $1`, key,
	).Scan(&n)
	if err != nil {
		t.Fatalf("count transfers: %v", err)
	}
	if n != 1 {
		t.Fatalf("transfers rows for key: got %d, want 1", n)
	}

	if mustGet(t, svc, from.ID).Balance != 10_00-amount {
		t.Fatalf("from balance after concurrent replay: got %d, want %d",
			mustGet(t, svc, from.ID).Balance, 10_00-amount)
	}
	if mustGet(t, svc, to.ID).Balance != amount {
		t.Fatalf("to balance after concurrent replay: got %d, want %d",
			mustGet(t, svc, to.ID).Balance, amount)
	}
	assertBalancesSumZero(t, svc, from.ID, to.ID, treasury)
}
