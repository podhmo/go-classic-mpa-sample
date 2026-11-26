package store

// MaxListPage bounds accepted page numbers so (page-1)*perPage cannot
// overflow int when a caller passes a huge query value.
const MaxListPage = 1_000_000

// normalizePage applies the list endpoint defaults and rejects absurd page
// numbers back to page 1. Store methods normalize even when callers forgot
// to validate input themselves.
func normalizePage(page, perPage int) (int, int) {
	if page < 1 || page > MaxListPage {
		page = 1
	}
	if perPage <= 0 {
		perPage = 20
	}
	return page, perPage
}
