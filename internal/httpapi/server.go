// Package httpapi translates HTTP/JSON into calls to the ledger services. No business rules here.
package httpapi

import (
	"context"
	_ "embed"
	"net/http"
	"time"

	"ledger/internal/ledger"
)

//go:embed openapi.yaml
var openAPISpec []byte

// Ports consumed by the handlers; ledger.ExpenseRegistry and ledger.PaymentForecaster satisfy them.
type expenseRegistry interface {
	Register(ctx context.Context, in ledger.RegisterExpenseInput) (ledger.Expense, error)
	Cancel(ctx context.Context, expenseID int64, effectiveDate time.Time) (ledger.Expense, error)
	SkipInstallment(ctx context.Context, expenseID int64, installmentNumber int) (ledger.Expense, error)
	List(ctx context.Context, afterID int64, requestedLimit int) (ledger.ExpensePage, error)
}

type paymentForecaster interface {
	UpcomingPayments(ctx context.Context, req ledger.UpcomingPaymentsRequest) (ledger.UpcomingPaymentsPage, error)
	MonthlyForecast(ctx context.Context, from time.Time, months int) (ledger.MonthlyForecast, error)
}

type server struct {
	registry   expenseRegistry
	forecaster paymentForecaster
}

func NewHandler(registry expenseRegistry, forecaster paymentForecaster) http.Handler {
	s := &server{registry, forecaster}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /expenses", s.registerExpense)
	mux.HandleFunc("GET /expenses", s.listExpenses)
	mux.HandleFunc("POST /expenses/{id}/cancellation", s.cancelExpense)
	mux.HandleFunc("POST /expenses/{id}/skipped-installments", s.skipInstallment)
	mux.HandleFunc("GET /upcoming-payments", s.listUpcomingPayments)
	mux.HandleFunc("GET /monthly-forecast", s.getMonthlyForecast)
	mux.HandleFunc("GET /openapi.yaml", serveOpenAPISpec)
	return recoverFromPanics(logRequests(mux))
}

func serveOpenAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(openAPISpec)
}
