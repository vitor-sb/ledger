package ledger

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

type fixedExpenses []Expense

func (f fixedExpenses) ListAll(context.Context) ([]Expense, error) { return f, nil }

func newForecaster() *PaymentForecaster {
	expenses := fixedExpenses{
		{ID: 1, Title: "Rent", AmountCents: 100000, FirstDueDate: date("2026-10-05"), Recurrence: RecurrenceMonthly},
		{ID: 2, Title: "Laptop", AmountCents: 30000, FirstDueDate: date("2026-11-10"), Recurrence: RecurrenceMonthly, InstallmentCount: 2},
		{ID: 3, Title: "Car tax", AmountCents: 50000, FirstDueDate: date("2026-12-20"), Recurrence: RecurrenceOnce},
	}
	return NewPaymentForecaster(expenses, func() time.Time { return date("2026-11-01") })
}

func TestMonthlyForecast(t *testing.T) {
	forecast, err := newForecaster().MonthlyForecast(context.Background(), time.Time{}, 3)
	if err != nil {
		t.Fatal(err)
	}
	// Nov: rent + laptop | Dec: rent + laptop + car tax | Jan: rent
	wantTotals := []int64{130000, 180000, 100000}
	if forecast.TotalCents != 410000 || len(forecast.Months) != 3 {
		t.Fatalf("forecast = %+v", forecast)
	}
	for i, month := range forecast.Months {
		if month.TotalCents != wantTotals[i] {
			t.Errorf("%s total = %d, want %d", month.Month, month.TotalCents, wantTotals[i])
		}
	}
	if forecast.Period.From != date("2026-11-01") || forecast.Period.To != date("2027-01-31") {
		t.Errorf("period = %+v", forecast.Period)
	}
}

func TestUpcomingPaymentsPaginationCoversEverythingOnce(t *testing.T) {
	forecaster := newForecaster()
	ctx := context.Background()

	full, err := forecaster.UpcomingPayments(ctx, UpcomingPaymentsRequest{Months: 3, Limit: MaxPageSize})
	if err != nil || full.Next != nil || len(full.Payments) != 6 {
		t.Fatalf("full list = %d payments, next=%v, err=%v; want 6 and no next", len(full.Payments), full.Next, err)
	}

	var walked []PaymentKey
	var cursor *PaymentKey
	for {
		page, err := forecaster.UpcomingPayments(ctx, UpcomingPaymentsRequest{Months: 3, Limit: 3, After: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range page.Payments {
			walked = append(walked, p.Key())
		}
		if page.Next == nil {
			break
		}
		cursor = page.Next
	}
	var want []PaymentKey
	for _, p := range full.Payments {
		want = append(want, p.Key())
	}
	if !slices.Equal(walked, want) {
		t.Errorf("paged walk = %v\nfull list  = %v", walked, want)
	}
	if !slices.IsSortedFunc(want, PaymentKey.Compare) {
		t.Error("payments are not sorted by key")
	}
}

func TestForecastRejectsInvalidMonths(t *testing.T) {
	for _, months := range []int{0, -1, MaxPeriodMonths + 1} {
		if _, err := newForecaster().MonthlyForecast(context.Background(), time.Time{}, months); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("months=%d err = %v, want ErrInvalidInput", months, err)
		}
	}
}
