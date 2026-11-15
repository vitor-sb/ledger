package ledger

import (
	"context"
	"fmt"
	"slices"
	"time"
)

// ExpenseSource is the read-only view the forecaster needs.
type ExpenseSource interface {
	ListAll(ctx context.Context) ([]Expense, error)
}

// PaymentForecaster answers "what will I pay and when", without changing anything.
type PaymentForecaster struct {
	source ExpenseSource
	clock  Clock
}

func NewPaymentForecaster(source ExpenseSource, clock Clock) *PaymentForecaster {
	return &PaymentForecaster{source: source, clock: clock}
}

type UpcomingPaymentsRequest struct {
	From   time.Time // zero = today
	Months int
	After  *PaymentKey // cursor: only payments strictly after this key
	Limit  int         // 0 = DefaultPageSize
}

type UpcomingPaymentsPage struct {
	Payments []ScheduledPayment
	Next     *PaymentKey // nil = no more pages
}

// UpcomingPayments lists payments in the period, ordered, with keyset pagination.
func (f *PaymentForecaster) UpcomingPayments(ctx context.Context, req UpcomingPaymentsRequest) (UpcomingPaymentsPage, error) {
	limit, err := resolvePageSize(req.Limit)
	if err != nil {
		return UpcomingPaymentsPage{}, err
	}
	_, payments, err := f.paymentsIn(ctx, req.From, req.Months)
	if err != nil {
		return UpcomingPaymentsPage{}, err
	}
	if req.After != nil {
		start, found := slices.BinarySearchFunc(payments, *req.After, func(p ScheduledPayment, target PaymentKey) int {
			return p.Key().Compare(target)
		})
		if found {
			start++
		}
		payments = payments[start:]
	}
	page := UpcomingPaymentsPage{Payments: payments}
	if len(payments) > limit {
		page.Payments = payments[:limit]
		next := payments[limit-1].Key()
		page.Next = &next
	}
	return page, nil
}

type MonthTotal struct {
	Month        string // YYYY-MM
	TotalCents   int64
	PaymentCount int
}

type MonthlyForecast struct {
	Period     Period
	TotalCents int64
	Months     []MonthTotal
}

// MonthlyForecast totals the payments of each month. Bounded by MaxPeriodMonths, so no pagination.
func (f *PaymentForecaster) MonthlyForecast(ctx context.Context, from time.Time, months int) (MonthlyForecast, error) {
	period, payments, err := f.paymentsIn(ctx, from, months)
	if err != nil {
		return MonthlyForecast{}, err
	}
	forecast := MonthlyForecast{Period: period, Months: []MonthTotal{}}
	for _, p := range payments {
		month := p.DueDate.Format("2006-01")
		if n := len(forecast.Months); n == 0 || forecast.Months[n-1].Month != month {
			forecast.Months = append(forecast.Months, MonthTotal{Month: month})
		}
		current := &forecast.Months[len(forecast.Months)-1]
		current.TotalCents += p.AmountCents
		current.PaymentCount++
		forecast.TotalCents += p.AmountCents
	}
	return forecast, nil
}

// paymentsIn loads every expense and expands it into the period, sorted by PaymentKey.
// NOTE: all in memory, fine for hundreds or thousands of expenses; push the expansion
// into SQL (generate_series) or cache if the volume grows.
func (f *PaymentForecaster) paymentsIn(ctx context.Context, from time.Time, months int) (Period, []ScheduledPayment, error) {
	if from.IsZero() {
		from = f.clock()
	}
	period, err := NewPeriod(from, months)
	if err != nil {
		return Period{}, nil, err
	}
	expenses, err := f.source.ListAll(ctx)
	if err != nil {
		return Period{}, nil, fmt.Errorf("list expenses: %w", err)
	}
	var payments []ScheduledPayment
	for _, expense := range expenses {
		payments = append(payments, expense.ScheduledPaymentsIn(period)...)
	}
	slices.SortFunc(payments, func(a, b ScheduledPayment) int { return a.Key().Compare(b.Key()) })
	return period, payments, nil
}
