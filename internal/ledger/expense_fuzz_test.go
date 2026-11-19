package ledger

import (
	"slices"
	"testing"
	"time"
)

var allRecurrences = []Recurrence{
	RecurrenceOnce, RecurrenceDaily, RecurrenceWeekly, RecurrenceBiweekly,
	RecurrenceMonthly, RecurrenceQuarterly, RecurrenceSemiannual, RecurrenceYearly,
}

func FuzzAddMonthsClamped(f *testing.F) {
	f.Add(int32(0), int16(1))   // 2026-01-01 + 1
	f.Add(int32(30), int16(1))  // Jan 31 + 1 month: the classic clamp
	f.Add(int32(58), int16(12)) // Feb 28 + a year
	f.Add(int32(1520), int16(-3))
	f.Fuzz(func(t *testing.T, dayOffset int32, months int16) {
		// Keep the range in something time.Time handles without overflowing.
		start := date("2026-01-01").AddDate(0, 0, int(dayOffset%20000))
		got := addMonthsClamped(start, int(months))

		wantMonth := time.Month((int(start.Month())-1+int(months))%12+12)%12 + 1
		if got.Month() != wantMonth {
			t.Fatalf("addMonthsClamped(%s, %d) landed in %s, want %s", start.Format("2006-01-02"), months, got.Month(), wantMonth)
		}
		lastDay := time.Date(got.Year(), got.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
		if got.Day() != min(start.Day(), lastDay) {
			t.Fatalf("addMonthsClamped(%s, %d) = %s, day should be min(%d, %d)", start.Format("2006-01-02"), months, got.Format("2006-01-02"), start.Day(), lastDay)
		}
	})
}

// FuzzScheduledPaymentsIn checks the expansion against properties that must hold for any
// expense, instead of against hand-picked dates.
func FuzzScheduledPaymentsIn(f *testing.F) {
	f.Add(uint8(4), int16(0), uint8(0), int16(-1), uint8(0), uint8(6))
	f.Add(uint8(1), int16(-400), uint8(0), int16(30), uint8(3), uint8(24))
	f.Add(uint8(4), int16(15), uint8(5), int16(-1), uint8(2), uint8(12))
	f.Fuzz(func(t *testing.T, recurrenceIndex uint8, firstDueOffset int16, installments uint8, cancelOffset int16, skipped uint8, months uint8) {
		expense := Expense{
			ID:               1,
			Title:            "fuzz",
			AmountCents:      100,
			FirstDueDate:     date("2026-11-01").AddDate(0, 0, int(firstDueOffset)),
			Recurrence:       allRecurrences[int(recurrenceIndex)%len(allRecurrences)],
			InstallmentCount: int(installments % 50),
		}
		if cancelOffset >= 0 {
			cancelled := date("2026-11-01").AddDate(0, 0, int(cancelOffset))
			expense.CancelledFrom = &cancelled
		}
		if skipped > 0 {
			expense.SkippedInstallments = []int{int(skipped)}
		}
		period, err := NewPeriod(date("2026-11-01"), int(months%MaxPeriodMonths)+1)
		if err != nil {
			t.Fatal(err)
		}

		payments := expense.ScheduledPaymentsIn(period)

		if len(payments) > expense.paymentsCapacity(period) {
			t.Fatalf("%d payments exceed the capacity hint %d", len(payments), expense.paymentsCapacity(period))
		}
		for i, p := range payments {
			if p.DueDate.Before(period.From) || p.DueDate.After(period.To) {
				t.Fatalf("payment %d due %s is outside %v", i, p.DueDate, period)
			}
			if expense.isCancelledAt(p.DueDate) || slices.Contains(expense.SkippedInstallments, p.InstallmentNumber) {
				t.Fatalf("payment %+v should have been cancelled or skipped", p)
			}
			if i > 0 && payments[i-1].Key().Compare(p.Key()) >= 0 {
				t.Fatalf("payments not strictly ascending at %d: %+v then %+v", i, payments[i-1], p)
			}
		}
	})
}
