package ledger

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func date(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestDueDateHasNoEndOfMonthDrift(t *testing.T) {
	expense := Expense{FirstDueDate: date("2026-01-31"), Recurrence: RecurrenceMonthly}
	want := map[int]string{1: "2026-01-31", 2: "2026-02-28", 3: "2026-03-31", 4: "2026-04-30"}
	for n, wantDate := range want {
		if got := expense.dueDateOf(n).Format("2006-01-02"); got != wantDate {
			t.Errorf("installment %d due %s, want %s", n, got, wantDate)
		}
	}
}

func TestNewExpenseRejectsInvalidInput(t *testing.T) {
	validDate := date("2026-11-05")
	tests := []struct {
		name             string
		title            string
		amountCents      int64
		firstDueDate     time.Time
		recurrence       Recurrence
		installmentCount int
	}{
		{"empty title", "", 100, validDate, RecurrenceMonthly, 0},
		{"title too long", strings.Repeat("x", 31), 100, validDate, RecurrenceMonthly, 0},
		{"zero amount", "Rent", 0, validDate, RecurrenceMonthly, 0},
		{"missing date", "Rent", 100, time.Time{}, RecurrenceMonthly, 0},
		{"unknown recurrence", "Rent", 100, validDate, "WEEKENDS", 0},
		{"negative installments", "Rent", 100, validDate, RecurrenceMonthly, -1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewExpense(tc.title, tc.amountCents, tc.firstDueDate, tc.recurrence, tc.installmentCount)
			if !errors.Is(err, ErrInvalidInput) {
				t.Errorf("err = %v, want ErrInvalidInput", err)
			}
		})
	}
	if _, err := NewExpense("Pão de queijo", 100, validDate, RecurrenceOnce, 0); err != nil {
		t.Errorf("valid expense rejected: %v", err)
	}
}

func TestScheduledPaymentsIn(t *testing.T) {
	cancelledFromDec := date("2026-12-01")
	tests := []struct {
		name         string
		expense      Expense
		from, to     string
		wantDueDates []string
		wantFirstNum int
	}{
		{"monthly, period starts mid-series",
			Expense{ID: 1, FirstDueDate: date("2026-10-05"), Recurrence: RecurrenceMonthly},
			"2026-11-01", "2027-01-31", []string{"2026-11-05", "2026-12-05", "2027-01-05"}, 2},
		{"installment count caps the series",
			Expense{ID: 2, FirstDueDate: date("2026-11-10"), Recurrence: RecurrenceMonthly, InstallmentCount: 2},
			"2026-11-01", "2027-12-31", []string{"2026-11-10", "2026-12-10"}, 1},
		{"one-off inside the period",
			Expense{ID: 3, FirstDueDate: date("2026-12-20"), Recurrence: RecurrenceOnce},
			"2026-01-01", "2027-12-31", []string{"2026-12-20"}, 1},
		{"one-off outside the period",
			Expense{ID: 4, FirstDueDate: date("2026-12-20"), Recurrence: RecurrenceOnce},
			"2027-01-01", "2027-12-31", nil, 0},
		{"biweekly",
			Expense{ID: 5, FirstDueDate: date("2026-11-01"), Recurrence: RecurrenceBiweekly},
			"2026-11-01", "2026-11-30", []string{"2026-11-01", "2026-11-15", "2026-11-29"}, 1},
		{"cancellation removes payments on or after the effective date",
			Expense{ID: 6, FirstDueDate: date("2026-10-01"), Recurrence: RecurrenceMonthly, CancelledFrom: &cancelledFromDec},
			"2026-10-01", "2027-03-31", []string{"2026-10-01", "2026-11-01"}, 1},
		{"skipped installment leaves a gap but keeps numbering",
			Expense{ID: 7, FirstDueDate: date("2026-11-01"), Recurrence: RecurrenceMonthly, SkippedInstallments: []int{2}},
			"2026-11-01", "2027-01-31", []string{"2026-11-01", "2027-01-01"}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.expense.ScheduledPaymentsIn(Period{From: date(tc.from), To: date(tc.to)})
			if len(got) != len(tc.wantDueDates) {
				t.Fatalf("got %d payments, want %d: %+v", len(got), len(tc.wantDueDates), got)
			}
			for i, payment := range got {
				if due := payment.DueDate.Format("2006-01-02"); due != tc.wantDueDates[i] {
					t.Errorf("payment %d due %s, want %s", i, due, tc.wantDueDates[i])
				}
			}
			if len(got) > 0 && got[0].InstallmentNumber != tc.wantFirstNum {
				t.Errorf("first installment number = %d, want %d", got[0].InstallmentNumber, tc.wantFirstNum)
			}
		})
	}
}

func TestHasInstallment(t *testing.T) {
	finite := Expense{Recurrence: RecurrenceMonthly, InstallmentCount: 3}
	openEnded := Expense{Recurrence: RecurrenceMonthly}
	once := Expense{Recurrence: RecurrenceOnce}
	cases := []struct {
		expense Expense
		n       int
		want    bool
	}{
		{finite, 0, false}, {finite, 3, true}, {finite, 4, false},
		{openEnded, 500, true}, {once, 1, true}, {once, 2, false},
	}
	for _, c := range cases {
		if got := c.expense.HasInstallment(c.n); got != c.want {
			t.Errorf("%+v.HasInstallment(%d) = %v, want %v", c.expense, c.n, got, c.want)
		}
	}
}
