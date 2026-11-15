package postgres

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"ledger/internal/ledger"
)

// Integration tests need a real Postgres (make db) and skip without TEST_DATABASE_URL.
// Each test runs in its own schema, dropped at the end.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("t_%d", rand.Int64())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	config, _ := pgxpool.ParseConfig(databaseURL)
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

func date(s string) time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return t
}

func createExpense(t *testing.T, repo *ExpenseRepository, title string, recurrence ledger.Recurrence, installments int) int64 {
	t.Helper()
	expense, err := ledger.NewExpense(title, 1000, date("2026-11-05"), recurrence, installments)
	if err != nil {
		t.Fatal(err)
	}
	id, err := repo.Create(context.Background(), expense)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestMigrateIsIdempotent(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(context.Background(), pool); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
}

func TestCreateAndFindByID(t *testing.T) {
	ctx := context.Background()
	repo := NewExpenseRepository(newTestPool(t))
	id := createExpense(t, repo, "Laptop", ledger.RecurrenceMonthly, 3)

	got, err := repo.FindByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Laptop" || got.InstallmentCount != 3 || got.Recurrence != ledger.RecurrenceMonthly ||
		got.FirstDueDate != date("2026-11-05") || got.CancelledFrom != nil || len(got.SkippedInstallments) != 0 {
		t.Errorf("got %+v", got)
	}
	if _, err := repo.FindByID(ctx, 9999); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("missing id err = %v, want ErrNotFound", err)
	}
}

func TestCancellationAndSkipsAreRecordedAndLoaded(t *testing.T) {
	ctx := context.Background()
	repo := NewExpenseRepository(newTestPool(t))
	id := createExpense(t, repo, "Rent", ledger.RecurrenceMonthly, 0)

	if err := repo.RecordCancellation(ctx, id, date("2027-01-01")); err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{3, 1} {
		if err := repo.RecordSkippedInstallment(ctx, id, n); err != nil {
			t.Fatal(err)
		}
	}
	got, err := repo.FindByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.CancelledFrom == nil || *got.CancelledFrom != date("2027-01-01") || !slices.Equal(got.SkippedInstallments, []int{1, 3}) {
		t.Errorf("got %+v", got)
	}
}

func TestConstraintViolationsBecomeDomainErrors(t *testing.T) {
	ctx := context.Background()
	repo := NewExpenseRepository(newTestPool(t))
	id := createExpense(t, repo, "Rent", ledger.RecurrenceMonthly, 0)

	_ = repo.RecordCancellation(ctx, id, date("2027-01-01"))
	if err := repo.RecordCancellation(ctx, id, date("2027-02-01")); !errors.Is(err, ledger.ErrConflict) {
		t.Errorf("duplicate cancellation err = %v, want ErrConflict", err)
	}
	_ = repo.RecordSkippedInstallment(ctx, id, 2)
	if err := repo.RecordSkippedInstallment(ctx, id, 2); !errors.Is(err, ledger.ErrConflict) {
		t.Errorf("duplicate skip err = %v, want ErrConflict", err)
	}
	if err := repo.RecordCancellation(ctx, 9999, date("2027-01-01")); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("unknown expense err = %v, want ErrNotFound", err)
	}
}

func TestListAfterKeysetPagination(t *testing.T) {
	ctx := context.Background()
	repo := NewExpenseRepository(newTestPool(t))
	var ids []int64
	for i := range 5 {
		ids = append(ids, createExpense(t, repo, fmt.Sprintf("e%d", i), ledger.RecurrenceOnce, 0))
	}
	page, err := repo.ListAfter(ctx, ids[1], 2)
	if err != nil || len(page) != 2 || page[0].ID != ids[2] || page[1].ID != ids[3] {
		t.Errorf("ListAfter = %+v, %v; want ids %v", page, err, ids[2:4])
	}
	all, err := repo.ListAll(ctx)
	if err != nil || len(all) != 5 {
		t.Errorf("ListAll = %d, %v; want 5", len(all), err)
	}
}

// End to end through the real services, repository included.
func TestForecastReflectsCancellationAndSkips(t *testing.T) {
	ctx := context.Background()
	repo := NewExpenseRepository(newTestPool(t))
	clock := func() time.Time { return date("2026-11-01") }
	registry := ledger.NewExpenseRegistry(repo, clock)
	forecaster := ledger.NewPaymentForecaster(repo, clock)

	rent, err := registry.Register(ctx, ledger.RegisterExpenseInput{
		Title: "Rent", AmountCents: 1000, FirstDueDate: date("2026-11-05"), Recurrence: ledger.RecurrenceMonthly,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.SkipInstallment(ctx, rent.ID, 2); err != nil { // skips December
		t.Fatal(err)
	}
	if _, err := registry.Cancel(ctx, rent.ID, date("2027-02-01")); err != nil { // no payments from February
		t.Fatal(err)
	}

	forecast, err := forecaster.MonthlyForecast(ctx, time.Time{}, 6)
	if err != nil {
		t.Fatal(err)
	}
	// Nov + Jan only (Dec skipped, Feb onwards cancelled)
	if forecast.TotalCents != 2000 || len(forecast.Months) != 2 {
		t.Errorf("forecast = %+v, want 2000 cents over 2 months", forecast)
	}
}
