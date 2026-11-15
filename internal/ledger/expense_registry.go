package ledger

import (
	"context"
	"fmt"
	"slices"
	"time"
)

// Clock is injected so "today" is controllable in tests.
type Clock func() time.Time

// ExpenseStore is what ExpenseRegistry needs from persistence (declared here, by the consumer).
type ExpenseStore interface {
	Create(ctx context.Context, expense Expense) (id int64, err error)
	FindByID(ctx context.Context, id int64) (Expense, error) // ErrNotFound when missing
	// ListAfter returns up to `limit` expenses with ID > afterID, ordered by ID (keyset pagination).
	ListAfter(ctx context.Context, afterID int64, limit int) ([]Expense, error)
	RecordCancellation(ctx context.Context, expenseID int64, effectiveDate time.Time) error     // ErrConflict if already cancelled
	RecordSkippedInstallment(ctx context.Context, expenseID int64, installmentNumber int) error // ErrConflict if already skipped
}

// ExpenseRegistry changes the state of expenses: register, cancel, skip an installment, list.
type ExpenseRegistry struct {
	store ExpenseStore
	clock Clock
}

func NewExpenseRegistry(store ExpenseStore, clock Clock) *ExpenseRegistry {
	return &ExpenseRegistry{store: store, clock: clock}
}

type RegisterExpenseInput struct {
	Title            string
	AmountCents      int64
	FirstDueDate     time.Time
	Recurrence       Recurrence
	InstallmentCount int
}

func (r *ExpenseRegistry) Register(ctx context.Context, in RegisterExpenseInput) (Expense, error) {
	expense, err := NewExpense(in.Title, in.AmountCents, in.FirstDueDate, in.Recurrence, in.InstallmentCount)
	if err != nil {
		return Expense{}, err
	}
	if expense.ID, err = r.store.Create(ctx, expense); err != nil {
		return Expense{}, fmt.Errorf("create expense: %w", err)
	}
	return expense, nil
}

// Cancel stops every payment due on or after effectiveDate (zero = today).
// Earlier payments stay in the history.
func (r *ExpenseRegistry) Cancel(ctx context.Context, expenseID int64, effectiveDate time.Time) (Expense, error) {
	expense, err := r.store.FindByID(ctx, expenseID)
	if err != nil {
		return Expense{}, err
	}
	if expense.CancelledFrom != nil {
		return Expense{}, fmt.Errorf("%w: expense %d is already cancelled", ErrConflict, expenseID)
	}
	if effectiveDate.IsZero() {
		effectiveDate = r.clock()
	}
	effectiveDate = startOfDayUTC(effectiveDate)
	if err := r.store.RecordCancellation(ctx, expenseID, effectiveDate); err != nil {
		return Expense{}, fmt.Errorf("cancel expense: %w", err)
	}
	expense.CancelledFrom = &effectiveDate
	return expense, nil
}

// SkipInstallment drops a single installment (e.g. the month you will not pay) without
// touching the rest of the series.
func (r *ExpenseRegistry) SkipInstallment(ctx context.Context, expenseID int64, installmentNumber int) (Expense, error) {
	expense, err := r.store.FindByID(ctx, expenseID)
	if err != nil {
		return Expense{}, err
	}
	if !expense.HasInstallment(installmentNumber) {
		return Expense{}, invalidInputf("expense %d has no installment %d", expenseID, installmentNumber)
	}
	if slices.Contains(expense.SkippedInstallments, installmentNumber) {
		return Expense{}, fmt.Errorf("%w: installment %d is already skipped", ErrConflict, installmentNumber)
	}
	if err := r.store.RecordSkippedInstallment(ctx, expenseID, installmentNumber); err != nil {
		return Expense{}, fmt.Errorf("skip installment: %w", err)
	}
	expense.SkippedInstallments = append(expense.SkippedInstallments, installmentNumber)
	return expense, nil
}

type ExpensePage struct {
	Expenses    []Expense
	NextAfterID int64 // 0 = no more pages; otherwise pass it back as afterID
}

// List pages through expenses by ID. requestedLimit 0 = DefaultPageSize.
func (r *ExpenseRegistry) List(ctx context.Context, afterID int64, requestedLimit int) (ExpensePage, error) {
	limit, err := resolvePageSize(requestedLimit)
	if err != nil {
		return ExpensePage{}, err
	}
	expenses, err := r.store.ListAfter(ctx, afterID, limit+1) // +1 reveals whether there is a next page
	if err != nil {
		return ExpensePage{}, fmt.Errorf("list expenses: %w", err)
	}
	page := ExpensePage{Expenses: expenses}
	if len(expenses) > limit {
		page.Expenses = expenses[:limit]
		page.NextAfterID = expenses[limit-1].ID
	}
	return page, nil
}
