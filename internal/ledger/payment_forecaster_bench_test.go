package ledger

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"
)

// syntheticExpenses builds n expenses that cycle through every recurrence, with first
// due dates spread over the last two years so the daily ones have a long history to skip.
func syntheticExpenses(n int) fixedExpenses {
	recurrences := []Recurrence{
		RecurrenceMonthly, RecurrenceWeekly, RecurrenceDaily, RecurrenceBiweekly,
		RecurrenceQuarterly, RecurrenceYearly, RecurrenceOnce, RecurrenceSemiannual,
	}
	base := date("2024-11-01")
	expenses := make(fixedExpenses, n)
	for i := range expenses {
		expenses[i] = Expense{
			ID:           int64(i + 1),
			Title:        fmt.Sprintf("expense %d", i+1),
			AmountCents:  int64(1000 + i%500),
			FirstDueDate: base.AddDate(0, 0, i%700),
			Recurrence:   recurrences[i%len(recurrences)],
		}
	}
	return expenses
}

// BenchmarkMonthlyForecast compares the sequential path (workers=1) with the concurrent one
// at several sizes. To see the effect of the core count: go test -bench . -cpu 1,2,4
func BenchmarkMonthlyForecast(b *testing.B) {
	for _, n := range []int{100, 1_000, 10_000, 50_000} {
		for _, workers := range []int{1, runtime.GOMAXPROCS(0)} {
			b.Run(fmt.Sprintf("expenses=%d/workers=%d", n, workers), func(b *testing.B) {
				forecaster := NewPaymentForecaster(syntheticExpenses(n), func() time.Time { return date("2026-11-01") })
				forecaster.workers = workers
				ctx := context.Background()
				b.ReportAllocs()
				for b.Loop() {
					if _, err := forecaster.MonthlyForecast(ctx, time.Time{}, MaxPeriodMonths); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// BenchmarkScheduledPaymentsIn isolates the per-expense expansion, which is what
// dominates paymentsIn. The daily case is the worst one: it walks every installment
// since the first due date.
func BenchmarkScheduledPaymentsIn(b *testing.B) {
	period, err := NewPeriod(date("2026-11-01"), 12)
	if err != nil {
		b.Fatal(err)
	}
	for _, recurrence := range []Recurrence{RecurrenceDaily, RecurrenceMonthly, RecurrenceOnce} {
		b.Run(string(recurrence), func(b *testing.B) {
			expense := Expense{ID: 1, Title: "bench", AmountCents: 1000, FirstDueDate: date("2024-11-01"), Recurrence: recurrence}
			b.ReportAllocs()
			for b.Loop() {
				_ = expense.ScheduledPaymentsIn(period)
			}
		})
	}
}
