package httpapi

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"ledger/internal/ledger"
)

// Cursors are opaque to clients: base64url of the last key seen.
// Clients must not build them; they only pass back what `next_cursor` gave.

var errInvalidCursor = errors.New("after: invalid cursor")

func encodeExpenseCursor(lastExpenseID int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(lastExpenseID, 10)))
}

// decodeExpenseCursor returns 0 for an empty cursor (first page).
func decodeExpenseCursor(cursor string) (int64, error) {
	if cursor == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, errInvalidCursor
	}
	id, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || id < 1 {
		return 0, errInvalidCursor
	}
	return id, nil
}

func encodePaymentCursor(key ledger.PaymentKey) string {
	raw := fmt.Sprintf("%s|%d|%d", key.DueDate.Format(dateLayout), key.ExpenseID, key.InstallmentNumber)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodePaymentCursor returns nil for an empty cursor (first page).
func decodePaymentCursor(cursor string) (*ledger.PaymentKey, error) {
	if cursor == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, errInvalidCursor
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 {
		return nil, errInvalidCursor
	}
	dueDate, dateErr := time.Parse(dateLayout, parts[0])
	expenseID, idErr := strconv.ParseInt(parts[1], 10, 64)
	installment, nErr := strconv.Atoi(parts[2])
	if dateErr != nil || idErr != nil || nErr != nil {
		return nil, errInvalidCursor
	}
	return &ledger.PaymentKey{DueDate: dueDate, ExpenseID: expenseID, InstallmentNumber: installment}, nil
}
