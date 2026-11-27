package store

const (
	// MaxListPage bounds accepted page numbers so huge query values cannot
	// overflow pagination arithmetic.
	MaxListPage = 1_000_000
	// MaxListPerPage bounds accepted page sizes for the same reason.
	MaxListPerPage = 1_000_000
)

// normalizePage applies the list endpoint defaults and rejects absurd page
// numbers back to page 1. Store methods normalize even when callers forgot
// to validate input themselves.
func normalizePage(page, perPage int) (int, int) {
	if page < 1 || page > MaxListPage {
		page = 1
	}
	if perPage <= 0 {
		perPage = 20
	} else if perPage > MaxListPerPage {
		perPage = MaxListPerPage
	}
	return page, perPage
}
