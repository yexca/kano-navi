package shop

import (
	"context"
	"errors"
	"time"
)

// State is the read bookkeeping kept between runs. BlockedUntil is when a
// 429 answer's Retry-After passes; no read requests the shop before it.
type State struct {
	LastAttemptAt, LastSuccessAt, BlockedUntil time.Time
	LastError                                  string
}

// Options bound a read.
type Options struct {
	// RequestDelay spaces the requests of one read.
	RequestDelay time.Duration
	// Now reads the clock; nil is time.Now.
	Now func() time.Time
}

// DefaultRequestDelay is the pause between the requests of one read.
const DefaultRequestDelay = 3 * time.Second

type Adapter struct {
	Client  *Client
	Options Options
}

func NewAdapter(client *Client) *Adapter {
	return &Adapter{Client: client, Options: Options{RequestDelay: DefaultRequestDelay}}
}

// Skip reasons of a read that made no request.
const SkippedRateLimited = "rate_limited"

// Result is what one read found. Sales are the period groups its products
// touched; they are empty when it failed.
type Result struct {
	Sales []Sale
	State State
	// Skipped is set when the read made no request.
	Skipped string
	// Requested counts requests, Listed the products the listing returned,
	// Candidates the tagged products with a dated period, and Outcomes why
	// the other products are not sale periods.
	Requested, Listed, Candidates int
	Outcomes                      map[string]int
	// Pages are the catalog pages a full read requested, and ReachedEnd
	// reports that it read the last one.
	Pages       int
	ReachedEnd  bool
	Collections bool
}

func (a *Adapter) now() time.Time {
	if a.Options.Now != nil {
		return a.Options.Now()
	}
	return time.Now()
}

// Read lists the newest products (all false) or the whole catalog, at most
// MaxCatalogPages pages, and groups Kano's sale periods. The collection list
// is read at most once, and only when a product is in no stored sale of its
// period. Any failed request fails the read: it returns no sales, and a 429
// holds every read back until its Retry-After passes. A read during that
// wait makes no request.
func (a *Adapter) Read(ctx context.Context, state State, stored []Sale, all bool) (Result, error) {
	now := a.now()
	if state.BlockedUntil.After(now) {
		return Result{State: state, Skipped: SkippedRateLimited}, nil
	}
	state.LastAttemptAt = now
	out := Result{State: state, Outcomes: map[string]int{}}
	wait := func() error {
		if out.Requested == 0 || a.Options.RequestDelay <= 0 {
			return nil
		}
		timer := time.NewTimer(a.Options.RequestDelay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	}
	fail := func(err error) (Result, error) {
		if ctx.Err() != nil {
			return Result{State: state, Requested: out.Requested}, ctx.Err()
		}
		out.State.LastError = ErrorCode(err)
		var shopErr *Error
		if errors.As(err, &shopErr) && shopErr.RetryAfter > 0 {
			out.State.BlockedUntil = a.now().Add(shopErr.RetryAfter)
		}
		out.Sales = nil
		return out, err
	}

	products := []Product{}
	seen := map[int64]bool{}
	add := func(page []Product) {
		for _, p := range page {
			if !seen[p.ID] {
				seen[p.ID] = true
				products = append(products, p)
			}
		}
	}
	if all {
		for page := 1; page <= MaxCatalogPages; page++ {
			if err := wait(); err != nil {
				return fail(err)
			}
			out.Requested++
			out.Pages++
			listed, size, err := a.Client.CatalogPage(ctx, page)
			if err != nil {
				return fail(err)
			}
			add(listed)
			// The page's own size ends the catalog, so a product left out
			// as unusable does not cut a full page short.
			if size < CatalogPageSize {
				out.ReachedEnd = true
				break
			}
		}
	} else {
		out.Requested++
		listed, err := a.Client.NewArrivals(ctx, ArrivalsLimit)
		if err != nil {
			return fail(err)
		}
		add(listed)
	}
	out.Listed = len(products)
	candidates, outcomes := Candidates(products)
	out.Candidates, out.Outcomes = len(candidates), outcomes
	var collections []Collection
	if NeedsCollections(candidates, stored) {
		if err := wait(); err != nil {
			return fail(err)
		}
		out.Requested++
		out.Collections = true
		listed, err := a.Client.Collections(ctx)
		if err != nil {
			return fail(err)
		}
		collections = listed
	}
	out.Sales = Group(candidates, stored, collections)
	out.State.LastSuccessAt, out.State.LastError, out.State.BlockedUntil = now, "", time.Time{}
	return out, nil
}
