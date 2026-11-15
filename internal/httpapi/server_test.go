package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ledger/internal/ledger"
)

type fakeRegistry struct{ err error }

func (f fakeRegistry) Register(_ context.Context, in ledger.RegisterExpenseInput) (ledger.Expense, error) {
	if f.err != nil {
		return ledger.Expense{}, f.err
	}
	e, err := ledger.NewExpense(in.Title, in.AmountCents, in.FirstDueDate, in.Recurrence, in.InstallmentCount)
	e.ID = 7
	return e, err
}
func (f fakeRegistry) Cancel(_ context.Context, id int64, effective time.Time) (ledger.Expense, error) {
	return ledger.Expense{ID: id, Title: "Rent", CancelledFrom: &effective}, f.err
}
func (f fakeRegistry) SkipInstallment(_ context.Context, id int64, n int) (ledger.Expense, error) {
	return ledger.Expense{ID: id, Title: "Rent", SkippedInstallments: []int{n}}, f.err
}
func (f fakeRegistry) List(_ context.Context, afterID int64, _ int) (ledger.ExpensePage, error) {
	return ledger.ExpensePage{Expenses: []ledger.Expense{{ID: afterID + 1, Title: "Rent"}}, NextAfterID: afterID + 1}, f.err
}

type fakeForecaster struct{ err error }

func (f fakeForecaster) UpcomingPayments(context.Context, ledger.UpcomingPaymentsRequest) (ledger.UpcomingPaymentsPage, error) {
	next := ledger.PaymentKey{DueDate: time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC), ExpenseID: 1, InstallmentNumber: 1}
	return ledger.UpcomingPaymentsPage{Next: &next}, f.err
}
func (f fakeForecaster) MonthlyForecast(_ context.Context, from time.Time, _ int) (ledger.MonthlyForecast, error) {
	return ledger.MonthlyForecast{TotalCents: 100, Months: []ledger.MonthTotal{{Month: "2026-11", TotalCents: 100, PaymentCount: 1}}}, f.err
}

func call(h http.Handler, method, url, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, url, strings.NewReader(body)))
	return rec
}

func TestEndpoints(t *testing.T) {
	healthy := NewHandler(fakeRegistry{}, fakeForecaster{})
	validExpense := `{"title":"Rent","amount_cents":100,"first_due_date":"2026-11-05","recurrence":"MENSAL"}`
	tests := []struct {
		name         string
		handler      http.Handler
		method, url  string
		body         string
		wantStatus   int
		wantContains string
	}{
		{"register ok", healthy, "POST", "/expenses", validExpense, 201, `"id":7`},
		{"register: domain validation is 422", healthy, "POST", "/expenses", `{"title":"","amount_cents":100,"first_due_date":"2026-11-05","recurrence":"MENSAL"}`, 422, "title"},
		{"register: unknown field is 400", healthy, "POST", "/expenses", `{"title":"x","bogus":1}`, 400, "malformed"},
		{"register: bad date is 400", healthy, "POST", "/expenses", `{"title":"x","first_due_date":"05/11/2026"}`, 400, "YYYY-MM-DD"},
		{"list returns next_cursor", healthy, "GET", "/expenses?limit=1", "", 200, `"next_cursor"`},
		{"list: invalid cursor is 400", healthy, "GET", "/expenses?after=@@@", "", 400, "invalid cursor"},
		{"cancel with empty body", healthy, "POST", "/expenses/3/cancellation", "", 201, `"cancelled_from"`},
		{"cancel: bad id is 400", healthy, "POST", "/expenses/abc/cancellation", "", 400, "id"},
		{"cancel: unknown expense is 404", NewHandler(fakeRegistry{err: ledger.ErrNotFound}, fakeForecaster{}), "POST", "/expenses/3/cancellation", "", 404, "not found"},
		{"cancel: already cancelled is 409", NewHandler(fakeRegistry{err: ledger.ErrConflict}, fakeForecaster{}), "POST", "/expenses/3/cancellation", "", 409, "conflict"},
		{"skip installment", healthy, "POST", "/expenses/3/skipped-installments", `{"installment_number":2}`, 201, `"skipped_installments":[2]`},
		{"upcoming payments with cursor", healthy, "GET", "/upcoming-payments?months=2", "", 200, `"next_cursor"`},
		{"upcoming payments: bad cursor", healthy, "GET", "/upcoming-payments?after=Zm9v", "", 400, "invalid cursor"},
		{"monthly forecast", healthy, "GET", "/monthly-forecast?from=2026-11-01&months=1", "", 200, `"total_cents":100`},
		{"months not an integer", healthy, "GET", "/monthly-forecast?months=abc", "", 400, "integer"},
		{"serves the OpenAPI spec", healthy, "GET", "/openapi.yaml", "", 200, "openapi: 3.0.3"},
		{"unexpected error hides details", NewHandler(fakeRegistry{}, fakeForecaster{err: errors.New("pg: password=secret")}), "GET", "/monthly-forecast", "", 500, "internal error"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := call(tc.handler, tc.method, tc.url, tc.body)
			if rec.Code != tc.wantStatus || !strings.Contains(rec.Body.String(), tc.wantContains) {
				t.Errorf("got %d %q; want %d containing %q", rec.Code, rec.Body, tc.wantStatus, tc.wantContains)
			}
			if strings.Contains(rec.Body.String(), "secret") {
				t.Error("internal error leaked to the client")
			}
		})
	}
}

func TestCursorsRoundTrip(t *testing.T) {
	if id, err := decodeExpenseCursor(encodeExpenseCursor(42)); err != nil || id != 42 {
		t.Errorf("expense cursor = %d, %v", id, err)
	}
	key := ledger.PaymentKey{DueDate: time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC), ExpenseID: 9, InstallmentNumber: 3}
	got, err := decodePaymentCursor(encodePaymentCursor(key))
	if err != nil || got == nil || got.Compare(key) != 0 {
		t.Errorf("payment cursor = %+v, %v", got, err)
	}
}
