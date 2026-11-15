package httpapi

import (
	"net/http"
	"time"

	"ledger/internal/ledger"
)

const defaultForecastMonths = 3

type scheduledPaymentResponse struct {
	ExpenseID         int64  `json:"expense_id"`
	Title             string `json:"title"`
	DueDate           string `json:"due_date"`
	AmountCents       int64  `json:"amount_cents"`
	InstallmentNumber int    `json:"installment_number"`
}

type upcomingPaymentsResponse struct {
	Items      []scheduledPaymentResponse `json:"items"`
	NextCursor string                     `json:"next_cursor,omitempty"`
}

type monthTotalResponse struct {
	Month        string `json:"month"`
	TotalCents   int64  `json:"total_cents"`
	PaymentCount int    `json:"payment_count"`
}

type monthlyForecastResponse struct {
	From       string               `json:"from"`
	To         string               `json:"to"`
	TotalCents int64                `json:"total_cents"`
	Months     []monthTotalResponse `json:"months"`
}

// forecastParams reads the shared ?from=YYYY-MM-DD&months=N parameters.
func forecastParams(w http.ResponseWriter, r *http.Request) (from time.Time, months int, ok bool) {
	query := r.URL.Query()
	if value := query.Get("from"); value != "" {
		if from, ok = parseDate(w, "from", value); !ok {
			return
		}
	}
	months, err := optionalInt(query.Get("months"), defaultForecastMonths)
	if err != nil {
		writeBadRequest(w, "months: must be an integer")
		return from, 0, false
	}
	return from, months, true
}

// GET /upcoming-payments?from=&months=&limit=&after=<cursor>
func (s *server) listUpcomingPayments(w http.ResponseWriter, r *http.Request) {
	from, months, ok := forecastParams(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	after, err := decodePaymentCursor(query.Get("after"))
	if err != nil {
		writeBadRequest(w, err.Error())
		return
	}
	limit, err := optionalInt(query.Get("limit"), 0)
	if err != nil {
		writeBadRequest(w, "limit: must be an integer")
		return
	}
	page, err := s.forecaster.UpcomingPayments(r.Context(), ledger.UpcomingPaymentsRequest{
		From: from, Months: months, After: after, Limit: limit,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	resp := upcomingPaymentsResponse{Items: make([]scheduledPaymentResponse, 0, len(page.Payments))}
	for _, p := range page.Payments {
		resp.Items = append(resp.Items, scheduledPaymentResponse{
			p.ExpenseID, p.Title, p.DueDate.Format(dateLayout), p.AmountCents, p.InstallmentNumber,
		})
	}
	if page.Next != nil {
		resp.NextCursor = encodePaymentCursor(*page.Next)
	}
	writeJSON(w, http.StatusOK, resp)
}

// GET /monthly-forecast?from=&months=
func (s *server) getMonthlyForecast(w http.ResponseWriter, r *http.Request) {
	from, months, ok := forecastParams(w, r)
	if !ok {
		return
	}
	forecast, err := s.forecaster.MonthlyForecast(r.Context(), from, months)
	if err != nil {
		writeError(w, err)
		return
	}
	resp := monthlyForecastResponse{
		From: forecast.Period.From.Format(dateLayout), To: forecast.Period.To.Format(dateLayout),
		TotalCents: forecast.TotalCents, Months: make([]monthTotalResponse, 0, len(forecast.Months)),
	}
	for _, m := range forecast.Months {
		resp.Months = append(resp.Months, monthTotalResponse{m.Month, m.TotalCents, m.PaymentCount})
	}
	writeJSON(w, http.StatusOK, resp)
}
