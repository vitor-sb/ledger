package ledger

import (
	"context"
	"slices"

	"golang.org/x/sync/errgroup"
)

// minExpensesPerWorker keeps small inputs on a single goroutine: below this, starting
// goroutines and merging costs more than the expansion itself. Picked from
// BenchmarkMonthlyForecast, not from theory; rerun it before changing.
const minExpensesPerWorker = 128

func comparePayments(a, b ScheduledPayment) int { return a.Key().Compare(b.Key()) }

// expandPayments expands every expense into the period and returns the payments sorted by
// PaymentKey.
//
// The expenses are cut in contiguous chunks, one goroutine per chunk. Each goroutine expands
// and sorts only its chunk, which is where nearly all the time goes, and the sorted runs
// are merged at the end. PaymentKey is a total order with no ties (expense and installment
// are part of it), so the result is the same whatever the number of workers.
func expandPayments(ctx context.Context, expenses []Expense, period Period, workers int) ([]ScheduledPayment, error) {
	workers = max(1, min(workers, len(expenses)/minExpensesPerWorker))
	if workers == 1 {
		return expandChunk(ctx, expenses, period)
	}

	chunkSize := (len(expenses) + workers - 1) / workers
	var chunks [][]Expense
	for chunk := range slices.Chunk(expenses, chunkSize) {
		chunks = append(chunks, chunk)
	}

	// Each goroutine writes only its own index of runs, so no mutex is needed.
	runs := make([][]ScheduledPayment, len(chunks))
	group, groupCtx := errgroup.WithContext(ctx)
	for i, chunk := range chunks {
		group.Go(func() error {
			run, err := expandChunk(groupCtx, chunk, period)
			runs[i] = run
			return err
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	return mergeRuns(runs), nil
}

// expandChunk is the sequential core: expand, then sort. It checks the context between
// expenses so a cancelled request stops burning CPU.
func expandChunk(ctx context.Context, expenses []Expense, period Period) ([]ScheduledPayment, error) {
	capacity := 0
	for _, expense := range expenses {
		capacity += expense.paymentsCapacity(period)
	}
	payments := make([]ScheduledPayment, 0, capacity)
	for _, expense := range expenses {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		payments = expense.appendScheduledPaymentsIn(payments, period)
	}
	slices.SortFunc(payments, comparePayments)
	return payments, nil
}

// mergeRuns folds sorted runs into one, merging neighbours pairwise.
func mergeRuns(runs [][]ScheduledPayment) []ScheduledPayment {
	for len(runs) > 1 {
		var next [][]ScheduledPayment
		for i := 0; i < len(runs); i += 2 {
			if i+1 == len(runs) {
				next = append(next, runs[i]) // odd one out waits for the next round
				continue
			}
			next = append(next, mergeSorted(runs[i], runs[i+1]))
		}
		runs = next
	}
	if len(runs) == 0 {
		return nil
	}
	return runs[0]
}

func mergeSorted(a, b []ScheduledPayment) []ScheduledPayment {
	merged := make([]ScheduledPayment, 0, len(a)+len(b))
	for len(a) > 0 && len(b) > 0 {
		if comparePayments(a[0], b[0]) <= 0 {
			merged, a = append(merged, a[0]), a[1:]
		} else {
			merged, b = append(merged, b[0]), b[1:]
		}
	}
	merged = append(merged, a...)
	return append(merged, b...)
}
