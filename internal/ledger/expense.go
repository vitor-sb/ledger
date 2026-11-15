package ledger

import (
	"cmp"
	"slices"
	"time"
	"unicode/utf8"
)

const (
	MaxExpenseTitleLength = 30
	MaxPeriodMonths       = 24
)

// Expense is a commitment to pay something once or on a recurrence.
// Cancellations and skipped installments are records added over it, never deletions.
type Expense struct {
	ID               int64
	Title            string
	AmountCents      int64 // money in cents, never float
	FirstDueDate     time.Time
	Recurrence       Recurrence
	InstallmentCount int // 0 = open-ended (recurring expenses only)

	CancelledFrom       *time.Time // payments due on or after this date no longer happen
	SkippedInstallments []int      // 1-based numbers of installments that will not be paid
}

// NewExpense is the only way to build a valid Expense.
func NewExpense(title string, amountCents int64, firstDueDate time.Time, recurrence Recurrence, installmentCount int) (Expense, error) {
	switch {
	case title == "" || utf8.RuneCountInString(title) > MaxExpenseTitleLength:
		return Expense{}, invalidInputf("title must have 1 to %d characters", MaxExpenseTitleLength)
	case amountCents <= 0:
		return Expense{}, invalidInputf("amount_cents must be greater than 0")
	case firstDueDate.IsZero():
		return Expense{}, invalidInputf("first_due_date is required")
	case !recurrence.IsValid():
		return Expense{}, invalidInputf("unknown recurrence %q", recurrence)
	case installmentCount < 0:
		return Expense{}, invalidInputf("installment_count must be 0 or greater")
	}
	return Expense{
		Title: title, AmountCents: amountCents, FirstDueDate: startOfDayUTC(firstDueDate),
		Recurrence: recurrence, InstallmentCount: installmentCount,
	}, nil
}

// HasInstallment reports whether installment number n (1-based) exists for this expense.
func (e Expense) HasInstallment(n int) bool {
	switch {
	case n < 1:
		return false
	case e.Recurrence == RecurrenceOnce:
		return n == 1
	case e.InstallmentCount > 0:
		return n <= e.InstallmentCount
	}
	return true
}

// dueDateOf computes installment n's due date always from the first due date:
// adding month by month would accumulate the end-of-month clamp
// (Jan 31 -> Feb 28 -> Mar 28), computing from the base gives Mar 31.
func (e Expense) dueDateOf(n int) time.Time {
	step := recurrenceSteps[e.Recurrence]
	if step.days > 0 {
		return e.FirstDueDate.AddDate(0, 0, step.days*(n-1))
	}
	return addMonthsClamped(e.FirstDueDate, step.months*(n-1))
}

func (e Expense) isCancelledAt(dueDate time.Time) bool {
	return e.CancelledFrom != nil && !dueDate.Before(*e.CancelledFrom)
}

// Period is an inclusive range of calendar dates.
type Period struct{ From, To time.Time }

// NewPeriod covers `months` months starting at `from`.
func NewPeriod(from time.Time, months int) (Period, error) {
	if months < 1 || months > MaxPeriodMonths {
		return Period{}, invalidInputf("months must be between 1 and %d", MaxPeriodMonths)
	}
	from = startOfDayUTC(from)
	return Period{From: from, To: addMonthsClamped(from, months).AddDate(0, 0, -1)}, nil
}

type ScheduledPayment struct {
	ExpenseID         int64
	Title             string
	DueDate           time.Time
	AmountCents       int64
	InstallmentNumber int // 1-based
}

// PaymentKey is the total order of payments (due date, then expense, then installment).
// It is also the pagination cursor.
type PaymentKey struct {
	DueDate           time.Time
	ExpenseID         int64
	InstallmentNumber int
}

func (p ScheduledPayment) Key() PaymentKey {
	return PaymentKey{p.DueDate, p.ExpenseID, p.InstallmentNumber}
}

func (k PaymentKey) Compare(other PaymentKey) int {
	return cmp.Or(
		k.DueDate.Compare(other.DueDate),
		cmp.Compare(k.ExpenseID, other.ExpenseID),
		cmp.Compare(k.InstallmentNumber, other.InstallmentNumber),
	)
}

// ScheduledPaymentsIn expands the expense into the payments that fall inside the
// period, honoring cancellation and skipped installments.
// NOTE: iterates from the first installment, O(installments since the start);
// jump straight to the period if a very old daily expense ever becomes a bottleneck.
func (e Expense) ScheduledPaymentsIn(period Period) []ScheduledPayment {
	var payments []ScheduledPayment
	for n := 1; e.HasInstallment(n); n++ {
		due := e.dueDateOf(n)
		if due.After(period.To) || e.isCancelledAt(due) {
			break // due dates only grow, so nothing later can qualify
		}
		if !due.Before(period.From) && !slices.Contains(e.SkippedInstallments, n) {
			payments = append(payments, ScheduledPayment{e.ID, e.Title, due, e.AmountCents, n})
		}
	}
	return payments
}
