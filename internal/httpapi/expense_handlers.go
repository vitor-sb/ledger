package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"ledger/internal/ledger"
)

const dateLayout = "2006-01-02"

// JSON contract, kept apart from the domain types.
type registerExpenseRequest struct {
	Title            string `json:"title"`
	AmountCents      int64  `json:"amount_cents"`
	FirstDueDate     string `json:"first_due_date"`
	Recurrence       string `json:"recurrence"`
	InstallmentCount int    `json:"installment_count"`
}

type cancelExpenseRequest struct {
	EffectiveDate string `json:"effective_date"` // optional, default today
}

type skipInstallmentRequest struct {
	InstallmentNumber int `json:"installment_number"`
}

type expenseResponse struct {
	ID                  int64   `json:"id"`
	Title               string  `json:"title"`
	AmountCents         int64   `json:"amount_cents"`
	FirstDueDate        string  `json:"first_due_date"`
	Recurrence          string  `json:"recurrence"`
	InstallmentCount    int     `json:"installment_count,omitempty"`
	CancelledFrom       *string `json:"cancelled_from,omitempty"`
	SkippedInstallments []int   `json:"skipped_installments,omitempty"`
}

type expensePageResponse struct {
	Items      []expenseResponse `json:"items"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

func toExpenseResponse(e ledger.Expense) expenseResponse {
	resp := expenseResponse{
		ID: e.ID, Title: e.Title, AmountCents: e.AmountCents, FirstDueDate: e.FirstDueDate.Format(dateLayout),
		Recurrence: string(e.Recurrence), InstallmentCount: e.InstallmentCount, SkippedInstallments: e.SkippedInstallments,
	}
	if e.CancelledFrom != nil {
		formatted := e.CancelledFrom.Format(dateLayout)
		resp.CancelledFrom = &formatted
	}
	return resp
}

func (s *server) registerExpense(w http.ResponseWriter, r *http.Request) {
	var req registerExpenseRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}
	firstDueDate, ok := parseDate(w, "first_due_date", req.FirstDueDate)
	if !ok {
		return
	}
	expense, err := s.registry.Register(r.Context(), ledger.RegisterExpenseInput{
		Title: req.Title, AmountCents: req.AmountCents, FirstDueDate: firstDueDate,
		Recurrence: ledger.Recurrence(req.Recurrence), InstallmentCount: req.InstallmentCount,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toExpenseResponse(expense))
}

// GET /expenses?limit=20&after=<cursor>
func (s *server) listExpenses(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	afterID, err := decodeExpenseCursor(query.Get("after"))
	if err != nil {
		writeBadRequest(w, err.Error())
		return
	}
	limit, err := optionalInt(query.Get("limit"), 0)
	if err != nil {
		writeBadRequest(w, "limit: must be an integer")
		return
	}
	page, err := s.registry.List(r.Context(), afterID, limit)
	if err != nil {
		writeError(w, err)
		return
	}
	resp := expensePageResponse{Items: make([]expenseResponse, 0, len(page.Expenses))}
	for _, e := range page.Expenses {
		resp.Items = append(resp.Items, toExpenseResponse(e))
	}
	if page.NextAfterID != 0 {
		resp.NextCursor = encodeExpenseCursor(page.NextAfterID)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *server) cancelExpense(w http.ResponseWriter, r *http.Request) {
	expenseID, ok := parseExpenseID(w, r)
	if !ok {
		return
	}
	var req cancelExpenseRequest
	if !decodeOptionalJSONBody(w, r, &req) {
		return
	}
	var effectiveDate time.Time
	if req.EffectiveDate != "" {
		if effectiveDate, ok = parseDate(w, "effective_date", req.EffectiveDate); !ok {
			return
		}
	}
	expense, err := s.registry.Cancel(r.Context(), expenseID, effectiveDate)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toExpenseResponse(expense))
}

func (s *server) skipInstallment(w http.ResponseWriter, r *http.Request) {
	expenseID, ok := parseExpenseID(w, r)
	if !ok {
		return
	}
	var req skipInstallmentRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}
	expense, err := s.registry.SkipInstallment(r.Context(), expenseID, req.InstallmentNumber)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toExpenseResponse(expense))
}

func parseExpenseID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeBadRequest(w, "id: must be a positive integer")
		return 0, false
	}
	return id, true
}

func parseDate(w http.ResponseWriter, field, value string) (time.Time, bool) {
	date, err := time.Parse(dateLayout, value)
	if err != nil {
		writeBadRequest(w, field+": use YYYY-MM-DD")
		return time.Time{}, false
	}
	return date, true
}

func optionalInt(value string, defaultValue int) (int, error) {
	if value == "" {
		return defaultValue, nil
	}
	return strconv.Atoi(value)
}

// decodeJSONBody rejects unknown fields and bodies over 1 MiB.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeBadRequest(w, "malformed JSON: "+err.Error())
		return false
	}
	return true
}

// decodeOptionalJSONBody is like decodeJSONBody but accepts an empty body.
func decodeOptionalJSONBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil && !errors.Is(err, io.EOF) {
		writeBadRequest(w, "malformed JSON: "+err.Error())
		return false
	}
	return true
}
