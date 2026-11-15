package ledger

import "time"

// Recurrence values are in Portuguese on purpose: they are the domain vocabulary shared with
// financial-reminder-api, and they are the values the API and the database accept.
type Recurrence string

const (
	RecurrenceOnce       Recurrence = "UNICA"
	RecurrenceDaily      Recurrence = "DIARIA"
	RecurrenceWeekly     Recurrence = "SEMANAL"
	RecurrenceBiweekly   Recurrence = "QUINZENAL"
	RecurrenceMonthly    Recurrence = "MENSAL"
	RecurrenceQuarterly  Recurrence = "TRIMESTRAL"
	RecurrenceSemiannual Recurrence = "SEMESTRAL"
	RecurrenceYearly     Recurrence = "ANUAL"
)

// recurrenceSteps: step in days OR in months (never both).
var recurrenceSteps = map[Recurrence]struct{ days, months int }{
	RecurrenceOnce:       {},
	RecurrenceDaily:      {days: 1},
	RecurrenceWeekly:     {days: 7},
	RecurrenceBiweekly:   {days: 14},
	RecurrenceMonthly:    {months: 1},
	RecurrenceQuarterly:  {months: 3},
	RecurrenceSemiannual: {months: 6},
	RecurrenceYearly:     {months: 12},
}

func (r Recurrence) IsValid() bool { _, ok := recurrenceSteps[r]; return ok }

// startOfDayUTC normalizes a timestamp to a calendar date at 00:00 UTC.
func startOfDayUTC(t time.Time) time.Time {
	year, month, day := t.UTC().Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// addMonthsClamped adds months keeping the end of month (Jan 31 + 1 month = Feb 28),
// unlike time.AddDate, which normalizes to Mar 3.
func addMonthsClamped(t time.Time, months int) time.Time {
	year, month, day := t.Date()
	firstOfTarget := time.Date(year, month+time.Month(months), 1, 0, 0, 0, 0, time.UTC)
	lastDayOfTarget := firstOfTarget.AddDate(0, 1, -1).Day()
	return time.Date(firstOfTarget.Year(), firstOfTarget.Month(), min(day, lastDayOfTarget), 0, 0, 0, 0, time.UTC)
}
