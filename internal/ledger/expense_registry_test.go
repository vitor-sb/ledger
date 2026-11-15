package ledger

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

// inMemoryStore is a fake ExpenseStore that honors the same contract as the Postgres one.
type inMemoryStore struct {
	expenses []Expense
}

func (s *inMemoryStore) Create(_ context.Context, e Expense) (int64, error) {
	e.ID = int64(len(s.expenses) + 1)
	s.expenses = append(s.expenses, e)
	return e.ID, nil
}

func (s *inMemoryStore) FindByID(_ context.Context, id int64) (Expense, error) {
	for _, e := range s.expenses {
		if e.ID == id {
			return e, nil
		}
	}
	return Expense{}, ErrNotFound
}

func (s *inMemoryStore) ListAfter(_ context.Context, afterID int64, limit int) ([]Expense, error) {
	var out []Expense
	for _, e := range s.expenses {
		if e.ID > afterID && len(out) < limit {
			out = append(out, e)
		}
	}
	return out, nil
}

func (s *inMemoryStore) RecordCancellation(_ context.Context, id int64, effective time.Time) error {
	i := slices.IndexFunc(s.expenses, func(e Expense) bool { return e.ID == id })
	s.expenses[i].CancelledFrom = &effective
	return nil
}

func (s *inMemoryStore) RecordSkippedInstallment(_ context.Context, id int64, n int) error {
	i := slices.IndexFunc(s.expenses, func(e Expense) bool { return e.ID == id })
	s.expenses[i].SkippedInstallments = append(s.expenses[i].SkippedInstallments, n)
	return nil
}

func newRegistry() (*ExpenseRegistry, *inMemoryStore) {
	store := &inMemoryStore{}
	return NewExpenseRegistry(store, func() time.Time { return date("2026-11-15") }), store
}

func registerRent(t *testing.T, r *ExpenseRegistry) Expense {
	t.Helper()
	e, err := r.Register(context.Background(), RegisterExpenseInput{
		Title: "Rent", AmountCents: 150000, FirstDueDate: date("2026-11-05"), Recurrence: RecurrenceMonthly,
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestRegister(t *testing.T) {
	registry, store := newRegistry()
	expense := registerRent(t, registry)
	if expense.ID != 1 || len(store.expenses) != 1 {
		t.Errorf("expense = %+v, stored = %d", expense, len(store.expenses))
	}
	_, err := registry.Register(context.Background(), RegisterExpenseInput{Title: ""})
	if !errors.Is(err, ErrInvalidInput) || len(store.expenses) != 1 {
		t.Errorf("invalid input must not reach the store: err=%v stored=%d", err, len(store.expenses))
	}
}

func TestCancel(t *testing.T) {
	ctx := context.Background()
	registry, _ := newRegistry()
	registerRent(t, registry)

	cancelled, err := registry.Cancel(ctx, 1, time.Time{}) // zero = today (clock = 2026-11-15)
	if err != nil || cancelled.CancelledFrom == nil || cancelled.CancelledFrom.Format("2006-01-02") != "2026-11-15" {
		t.Fatalf("Cancel = %+v, %v; want effective 2026-11-15", cancelled, err)
	}
	if _, err := registry.Cancel(ctx, 1, time.Time{}); !errors.Is(err, ErrConflict) {
		t.Errorf("second cancel err = %v, want ErrConflict", err)
	}
	if _, err := registry.Cancel(ctx, 99, time.Time{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown expense err = %v, want ErrNotFound", err)
	}
}

func TestSkipInstallment(t *testing.T) {
	ctx := context.Background()
	registry, _ := newRegistry()
	_, err := registry.Register(ctx, RegisterExpenseInput{
		Title: "Laptop", AmountCents: 30000, FirstDueDate: date("2026-11-10"), Recurrence: RecurrenceMonthly, InstallmentCount: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if e, err := registry.SkipInstallment(ctx, 1, 2); err != nil || !slices.Equal(e.SkippedInstallments, []int{2}) {
		t.Fatalf("skip 2 = %+v, %v", e, err)
	}
	if _, err := registry.SkipInstallment(ctx, 1, 2); !errors.Is(err, ErrConflict) {
		t.Errorf("skip twice err = %v, want ErrConflict", err)
	}
	for _, n := range []int{0, 4} {
		if _, err := registry.SkipInstallment(ctx, 1, n); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("skip %d err = %v, want ErrInvalidInput", n, err)
		}
	}
}

func TestListPaginatesByKeyset(t *testing.T) {
	ctx := context.Background()
	registry, _ := newRegistry()
	for range 5 {
		registerRent(t, registry)
	}

	var seen []int64
	var afterID int64
	for pages := 0; ; pages++ {
		page, err := registry.List(ctx, afterID, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range page.Expenses {
			seen = append(seen, e.ID)
		}
		if page.NextAfterID == 0 {
			if pages != 2 {
				t.Errorf("last page index = %d, want 2 (sizes 2+2+1)", pages)
			}
			break
		}
		afterID = page.NextAfterID
	}
	if !slices.Equal(seen, []int64{1, 2, 3, 4, 5}) {
		t.Errorf("seen = %v, want 1..5 in order without repeats", seen)
	}
	if _, err := registry.List(ctx, 0, MaxPageSize+1); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("oversized limit err = %v, want ErrInvalidInput", err)
	}
}
