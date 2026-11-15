package ledger

const (
	DefaultPageSize = 20
	MaxPageSize     = 100
)

// resolvePageSize turns 0 (not informed) into the default and rejects out-of-range values.
func resolvePageSize(requested int) (int, error) {
	switch {
	case requested == 0:
		return DefaultPageSize, nil
	case requested < 0 || requested > MaxPageSize:
		return 0, invalidInputf("limit must be between 1 and %d", MaxPageSize)
	}
	return requested, nil
}
