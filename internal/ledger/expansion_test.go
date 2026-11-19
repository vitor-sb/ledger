package ledger

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"
)

// expandSequentially is the obvious implementation, kept as the reference the concurrent
// one has to agree with.
func expandSequentially(expenses []Expense, period Period) []ScheduledPayment {
	var payments []ScheduledPayment
	for _, expense := range expenses {
		payments = append(payments, expense.ScheduledPaymentsIn(period)...)
	}
	slices.SortFunc(payments, comparePayments)
	return payments
}

func TestExpandPaymentsMatchesSequentialReference(t *testing.T) {
	period, err := NewPeriod(date("2026-11-01"), 12)
	if err != nil {
		t.Fatal(err)
	}
	// Cancellations and skipped installments too, not only plain expenses.
	expenses := syntheticExpenses(2000)
	cancelled := date("2027-03-01")
	for i := 0; i < len(expenses); i += 7 {
		expenses[i].CancelledFrom = &cancelled
	}
	for i := 3; i < len(expenses); i += 11 {
		expenses[i].SkippedInstallments = []int{800, 801}
	}
	want := expandSequentially(expenses, period)
	if len(want) == 0 {
		t.Fatal("reference produced no payments; the test would prove nothing")
	}

	// 3, 5 and 7 leave an odd number of runs, which mergeRuns has to carry to the next round.
	for _, workers := range []int{1, 2, 3, 4, 5, 7, 16} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			got, err := expandPayments(context.Background(), expenses, period, workers)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, want) {
				t.Fatalf("got %d payments, want %d, or the order differs", len(got), len(want))
			}
		})
	}
}

func TestExpandPaymentsFewExpenses(t *testing.T) {
	period, _ := NewPeriod(date("2026-11-01"), 3)
	for _, n := range []int{0, 1, 5} {
		expenses := syntheticExpenses(n)
		got, err := expandPayments(context.Background(), expenses, period, 8)
		if err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}
		if want := expandSequentially(expenses, period); !slices.Equal(got, want) {
			t.Errorf("n=%d: got %d payments, want %d", n, len(got), len(want))
		}
	}
}

func TestExpandPaymentsStopsWhenContextIsCancelled(t *testing.T) {
	period, _ := NewPeriod(date("2026-11-01"), MaxPeriodMonths)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for _, workers := range []int{1, 4} {
		_, err := expandPayments(ctx, syntheticExpenses(1000), period, workers)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("workers=%d: err = %v, want context.Canceled", workers, err)
		}
	}
}

func TestMonthlyForecastReportsCancellation(t *testing.T) {
	forecaster := NewPaymentForecaster(syntheticExpenses(1000), func() time.Time { return date("2026-11-01") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := forecaster.MonthlyForecast(ctx, time.Time{}, 6); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestMergeRunsHandlesEmptyInput(t *testing.T) {
	if got := mergeRuns(nil); got != nil {
		t.Errorf("mergeRuns(nil) = %v, want nil", got)
	}
	one := []ScheduledPayment{{ExpenseID: 1, InstallmentNumber: 1}}
	if got := mergeRuns([][]ScheduledPayment{nil, one, nil}); !slices.Equal(got, one) {
		t.Errorf("merge with empty runs = %v, want %v", got, one)
	}
}
