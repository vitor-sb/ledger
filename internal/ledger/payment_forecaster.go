package ledger

import (
	"context"
	"fmt"
	"runtime"
	"slices"
	"time"
)

// ExpenseSource is the read-only view the forecaster needs.
type ExpenseSource interface {
	ListAll(ctx context.Context) ([]Expense, error)
}

// PaymentForecaster answers "what will I pay and when", without changing anything.
type PaymentForecaster struct {
	source  ExpenseSource
	clock   Clock
	workers int // goroutines used to expand expenses; 1 = sequential
}

func NewPaymentForecaster(source ExpenseSource, clock Clock) *PaymentForecaster {
	return &PaymentForecaster{source: source, clock: clock, workers: runtime.GOMAXPROCS(0)}
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
	// Payments come sorted, so a month change is detected by comparing year and month as
	// ints. Formatting the string for every payment allocated once per payment (the
	// profile showed it); now it is formatted once per month.
	var lastYear int
	var lastMonth time.Month
	for _, p := range payments {
		if year, month, _ := p.DueDate.Date(); len(forecast.Months) == 0 || year != lastYear || month != lastMonth {
			lastYear, lastMonth = year, month
			forecast.Months = append(forecast.Months, MonthTotal{Month: p.DueDate.Format("2006-01")})
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
	payments, err := expandPayments(ctx, expenses, period, f.workers)
	if err != nil {
		return Period{}, nil, fmt.Errorf("expand payments: %w", err)
	}
	return period, payments, nil
}
