package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"ledger/internal/ledger"
)

// ExpenseRepository implements ledger.ExpenseStore and ledger.ExpenseSource.
type ExpenseRepository struct{ pool *pgxpool.Pool }

func NewExpenseRepository(pool *pgxpool.Pool) *ExpenseRepository { return &ExpenseRepository{pool} }

const selectExpenses = `
	SELECT e.id, e.title, e.amount_cents, e.first_due_date, e.recurrence, e.installment_count,
	       c.effective_date,
	       ARRAY(SELECT s.installment_number FROM skipped_installments s
	              WHERE s.expense_id = e.id ORDER BY s.installment_number) AS skipped
	  FROM expenses e
	  LEFT JOIN expense_cancellations c ON c.expense_id = e.id`

func scanExpense(row pgx.CollectableRow) (ledger.Expense, error) {
	var (
		expense    ledger.Expense
		recurrence string
		skipped    []int32
	)
	err := row.Scan(&expense.ID, &expense.Title, &expense.AmountCents, &expense.FirstDueDate,
		&recurrence, &expense.InstallmentCount, &expense.CancelledFrom, &skipped)
	expense.Recurrence = ledger.Recurrence(recurrence)
	for _, n := range skipped {
		expense.SkippedInstallments = append(expense.SkippedInstallments, int(n))
	}
	return expense, err
}

func (r *ExpenseRepository) Create(ctx context.Context, expense ledger.Expense) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx,
		`INSERT INTO expenses (title, amount_cents, first_due_date, recurrence, installment_count)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		expense.Title, expense.AmountCents, expense.FirstDueDate, string(expense.Recurrence), expense.InstallmentCount,
	).Scan(&id)
	return id, err
}

func (r *ExpenseRepository) FindByID(ctx context.Context, id int64) (ledger.Expense, error) {
	rows, err := r.pool.Query(ctx, selectExpenses+` WHERE e.id = $1`, id)
	if err != nil {
		return ledger.Expense{}, err
	}
	expense, err := pgx.CollectExactlyOneRow(rows, scanExpense)
	if errors.Is(err, pgx.ErrNoRows) {
		return ledger.Expense{}, ledger.ErrNotFound
	}
	return expense, err
}

// ListAfter uses keyset pagination (WHERE id > $1 ... LIMIT): constant cost on any page.
func (r *ExpenseRepository) ListAfter(ctx context.Context, afterID int64, limit int) ([]ledger.Expense, error) {
	rows, err := r.pool.Query(ctx, selectExpenses+` WHERE e.id > $1 ORDER BY e.id LIMIT $2`, afterID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanExpense)
}

func (r *ExpenseRepository) ListAll(ctx context.Context) ([]ledger.Expense, error) {
	rows, err := r.pool.Query(ctx, selectExpenses+` ORDER BY e.id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanExpense)
}

func (r *ExpenseRepository) RecordCancellation(ctx context.Context, expenseID int64, effectiveDate time.Time) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO expense_cancellations (expense_id, effective_date) VALUES ($1, $2)`, expenseID, effectiveDate)
	return translateConstraintError(err)
}

func (r *ExpenseRepository) RecordSkippedInstallment(ctx context.Context, expenseID int64, installmentNumber int) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO skipped_installments (expense_id, installment_number) VALUES ($1, $2)`, expenseID, installmentNumber)
	return translateConstraintError(err)
}

// translateConstraintError maps Postgres integrity errors to domain errors,
// which also covers the race between the service's check and the insert.
func translateConstraintError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return ledger.ErrConflict
		case "23503": // foreign_key_violation
			return ledger.ErrNotFound
		}
	}
	return err
}
