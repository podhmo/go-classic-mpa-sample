package web

// pagerData is the shared pagination nav model. URLs are prebuilt by each
// list handler because the query shape differs across resource types.
type pagerData struct {
	Page       int
	TotalPages int
	PrevURL    string
	NextURL    string
}

func newPager(page, totalPages int, pageURL func(int) string) pagerData {
	p := pagerData{Page: page, TotalPages: totalPages}
	if page > 1 {
		p.PrevURL = pageURL(page - 1)
	}
	if page < totalPages {
		p.NextURL = pageURL(page + 1)
	}
	return p
}
