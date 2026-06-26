// pkg/plugin/optionchain.go
package plugin

import (
	"context"
	"fmt"
	"math"
	"time"

	yfmodels "github.com/wnjoon/go-yfinance/pkg/models"
	yfticker "github.com/wnjoon/go-yfinance/pkg/ticker"
)

// OptionRow is a flattened, side-tagged option contract row.
type OptionRow struct {
	Strike    float64
	Right     string // "C" | "P"
	Bid       float64
	Ask       float64
	LastPrice float64
	Currency  string
}

// OptionChainResult is the parsed chain returned by FetchOptionChain.
type OptionChainResult struct {
	Rows               []OptionRow
	Expiration         time.Time
	MarketState        string // e.g. "REGULAR", "CLOSED", "PRE", "POST"
	UnderlyingCurrency string
	QuoteTimeUs        int64 // unix microseconds of the quote snapshot
}

// optionRowsFromChain flattens calls and puts into a single OptionRow slice,
// tagging each row with its right ("C" or "P").
func optionRowsFromChain(calls, puts []yfmodels.Option) []OptionRow {
	rows := make([]OptionRow, 0, len(calls)+len(puts))
	for _, o := range calls {
		rows = append(rows, OptionRow{
			Strike:    o.Strike,
			Right:     "C",
			Bid:       o.Bid,
			Ask:       o.Ask,
			LastPrice: o.LastPrice,
			Currency:  o.Currency,
		})
	}
	for _, o := range puts {
		rows = append(rows, OptionRow{
			Strike:    o.Strike,
			Right:     "P",
			Bid:       o.Bid,
			Ask:       o.Ask,
			LastPrice: o.LastPrice,
			Currency:  o.Currency,
		})
	}
	return rows
}

// matchRow finds the first row matching strike (within epsilon) and right.
func matchRow(rows []OptionRow, strike float64, right string) (OptionRow, bool) {
	for _, r := range rows {
		if math.Abs(r.Strike-strike) < 1e-6 && r.Right == right {
			return r, true
		}
	}
	return OptionRow{}, false
}

// markFromRow returns the best available mark price for the row:
//   - mid-point (bid+ask)/2 when both bid and ask are > 0
//   - last traded price as fallback when mid is unavailable
//   - ok=false when no usable price exists
func markFromRow(r OptionRow) (float64, bool) {
	if r.Bid > 0 && r.Ask > 0 {
		return (r.Bid + r.Ask) / 2, true
	}
	if r.LastPrice > 0 {
		return r.LastPrice, true
	}
	return 0, false
}

// optionResultFromChain builds an OptionChainResult from a fetched chain,
// populating the underlying-derived gate/quote fields when present.
func optionResultFromChain(chain *yfmodels.OptionChain) *OptionChainResult {
	res := &OptionChainResult{
		Rows:       optionRowsFromChain(chain.Calls, chain.Puts),
		Expiration: chain.Expiration,
	}
	if chain.Underlying != nil {
		res.MarketState = chain.Underlying.MarketState
		res.UnderlyingCurrency = chain.Underlying.Currency
		res.QuoteTimeUs = chain.Underlying.RegularMarketTime * 1_000_000 // seconds → micros
	}
	return res
}

// FetchOptionChain fetches the option chain for the given underlying symbol at
// the specified expiry. The limiter token is consumed before the network call.
func (c *YfClient) FetchOptionChain(ctx context.Context, underlyingSymbol string, expiry time.Time) (*OptionChainResult, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}
	t, err := yfticker.New(underlyingSymbol)
	if err != nil {
		return nil, fmt.Errorf("ticker new %s: %w", underlyingSymbol, err)
	}
	chain, err := t.OptionChainAtExpiry(expiry)
	if err != nil {
		return nil, fmt.Errorf("option chain %s %s: %w", underlyingSymbol, expiry.Format("2006-01-02"), err)
	}
	if chain == nil {
		return nil, fmt.Errorf("nil option chain for %s at %s", underlyingSymbol, expiry.Format("2006-01-02"))
	}
	return optionResultFromChain(chain), nil
}
