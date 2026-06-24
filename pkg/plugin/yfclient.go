package plugin

import (
	"context"
	"fmt"
	"time"

	yflookup "github.com/wnjoon/go-yfinance/pkg/lookup"
	yfmodels "github.com/wnjoon/go-yfinance/pkg/models"
	yfrepair "github.com/wnjoon/go-yfinance/pkg/repair"
	yfticker "github.com/wnjoon/go-yfinance/pkg/ticker"
	"golang.org/x/time/rate"
)

// YfClient wraps github.com/wnjoon/go-yfinance with a token bucket. The
// lib itself does not enforce rate limiting (per its README), so every
// outbound call hits limiter.Wait first.
type YfClient struct {
	limiter *rate.Limiter
}

func NewYfClient(qps float64, burst int) *YfClient {
	if qps <= 0 {
		qps = 1
	}
	if burst <= 0 {
		burst = 3
	}
	return &YfClient{
		limiter: rate.NewLimiter(rate.Limit(qps), burst),
	}
}

// LookupCandidate matches the JSON shape the frontend already consumes
// (src/api/reference.ts LookupCandidate). Mirrors Python yfinance.Lookup
// row projection so the autocomplete UI stays unchanged.
type LookupCandidate struct {
	Symbol    string  `json:"symbol"`
	ShortName *string `json:"short_name"`
	Exchange  *string `json:"exchange"`
	Type      *string `json:"type"`
}

func (c *YfClient) Lookup(ctx context.Context, query string, limit int) ([]LookupCandidate, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}
	l, err := yflookup.New(query)
	if err != nil {
		return nil, fmt.Errorf("lookup new: %w", err)
	}
	docs, err := l.Stock(limit)
	if err != nil {
		return nil, fmt.Errorf("lookup stock: %w", err)
	}
	out := make([]LookupCandidate, 0, len(docs))
	for _, d := range docs {
		out = append(out, LookupCandidate{
			Symbol:    d.Symbol,
			ShortName: nilIfEmpty(d.ShortName),
			Exchange:  nilIfEmpty(d.Exchange),
			Type:      nilIfEmpty(d.QuoteType),
		})
	}
	return out, nil
}

// FetchBars returns the bar slice plus Yahoo's reported currency and a
// minor-unit reference price for the symbol. The reference price is the
// real "current" value Yahoo reports via FastInfo (regularMarketPrice or
// previousClose); for tickers Yahoo classifies as minor-unit (GBp/ZAc),
// the reference comes back in the same minor units even when the bar
// payload arrives in major units — that mismatch is what the per-bar
// log-distance classifier in normalizeOhlcvBar disambiguates.
//
// referencePrice == 0 means "no reference available" — caller falls back
// to the unconditional divisor (matches the Python behaviour for tickers
// whose fast_info lookup failed).
func (c *YfClient) FetchBars(ctx context.Context, symbol, barSize string, start, end time.Time) ([]yfmodels.Bar, string, string, string, float64, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, "", "", "", 0, err
	}
	interval := mapBarSize(barSize)
	if interval == "" {
		return nil, "", "", "", 0, fmt.Errorf("unsupported bar size: %s", barSize)
	}
	t, err := yfticker.New(symbol)
	if err != nil {
		return nil, "", "", "", 0, fmt.Errorf("ticker new %s: %w", symbol, err)
	}
	// AutoAdjust=false to mirror the Python publisher (close is raw,
	// AdjClose is split/dividend-adjusted). Repair runs explicitly below.
	bars, err := t.History(yfmodels.HistoryParams{
		Start:      &start,
		End:        &end,
		Interval:   interval,
		AutoAdjust: false,
		Actions:    true,
	})
	if err != nil {
		return nil, "", "", "", 0, fmt.Errorf("history %s %s: %w", symbol, barSize, err)
	}
	currency, resolvedSymbol, resolvedExchange := "", "", ""
	if meta := t.GetHistoryMetadata(); meta != nil {
		currency = meta.Currency
		resolvedSymbol = meta.Symbol
		resolvedExchange = meta.ExchangeName
	}
	// Run the wnjoon repairer to catch Yahoo's known 100x unit mixups
	// (random-day pence/pound swaps + permanent unit switches) and zero
	// fills. Doesn't help when the WHOLE history is in the wrong unit
	// (TFGS.L et al) — for that case the per-bar classifier in
	// normalizeOhlcvBar uses the reference price returned below.
	repairer := yfrepair.New(yfrepair.Options{
		Ticker:        symbol,
		Interval:      interval,
		Currency:      currency,
		FixUnitMixups: true,
		FixZeroes:     true,
	})
	if repaired, rerr := repairer.Repair(bars); rerr == nil {
		bars = repaired
	}
	// Reference price: only matters for tickers whose currency is in
	// the minor-unit map. fast_info delivers the price in the SAME unit
	// Yahoo's metadata claims, so the classifier can compare bar values
	// against a known-good anchor.
	var referencePrice float64
	if _, isMinor := minorUnitToMajor[currency]; isMinor {
		if fi, ferr := t.FastInfo(); ferr == nil && fi != nil {
			referencePrice = pickPositive(fi.LastPrice, fi.PreviousClose, fi.RegularMarketPreviousClose)
		}
	}
	return bars, currency, resolvedSymbol, resolvedExchange, referencePrice, nil
}

// Info fetches the company's sector + industry from Yahoo's quoteSummary
// (assetProfile module) — a different endpoint from FetchBars/FastInfo, so it
// costs its own limiter token. Empty strings are returned (not an error) when
// Yahoo has no profile for the symbol (common for ETFs, FX, crypto).
func (c *YfClient) Info(ctx context.Context, symbol string) (sector, industry string, err error) {
	if err = c.limiter.Wait(ctx); err != nil {
		return "", "", err
	}
	t, err := yfticker.New(symbol)
	if err != nil {
		return "", "", fmt.Errorf("ticker new %s: %w", symbol, err)
	}
	info, err := t.Info()
	if err != nil {
		return "", "", fmt.Errorf("info %s: %w", symbol, err)
	}
	if info == nil {
		return "", "", nil
	}
	return info.Sector, info.Industry, nil
}

func pickPositive(vs ...float64) float64 {
	for _, v := range vs {
		if v > 0 {
			return v
		}
	}
	return 0
}

func mapBarSize(s string) string {
	switch s {
	case "1m", "5m", "15m", "30m", "60m", "90m", "1h", "1d", "5d", "1wk", "1mo", "3mo", "2m":
		return s
	default:
		return ""
	}
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	v := s
	return &v
}
