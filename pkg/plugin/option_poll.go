// pkg/plugin/option_poll.go
package plugin

import (
	"context"
	"strconv"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
)

type chainFetchFn func(ctx context.Context, symbol string, expiry time.Time) (*OptionChainResult, error)

type heldOption struct {
	occ         OccParts
	occID       string
	portfolioID string
}

type chainGroup struct {
	symbol  string
	expiry  time.Time
	members []heldOption
}

// runOptionPollOnce performs one full poll pass and returns the number of marks
// published. fetch is injected for testability.
func runOptionPollOnce(ctx context.Context, client rwPGClient, pluginID string, fetch chainFetchFn) int {
	pairs, err := heldPairs(ctx, client)
	if err != nil {
		log.DefaultLogger.Warn("option poll: heldPairs failed", "err", err)
		return 0
	}

	// Build groups keyed by (yahooSymbol, expiry); resolve underlying per root.
	// Memoize resolveOptionUnderlying within this pass: one DB round-trip per
	// (root, portfolioID) instead of one per held contract.
	underlyingCache := map[string]underlyingMapping{}
	groups := map[string]*chainGroup{}
	for _, p := range pairs {
		if p.Kind != "option" {
			continue
		}
		parts, perr := ParseOcc(p.InstrumentID)
		if perr != nil {
			continue
		}
		if parts.Expiry.Before(time.Now().Truncate(24 * time.Hour)) {
			continue // expired
		}
		cacheKey := parts.Underlying + "|" + p.PortfolioID
		m, cached := underlyingCache[cacheKey]
		if !cached {
			var merr error
			m, merr = resolveOptionUnderlying(ctx, client, parts.Underlying, p.PortfolioID)
			if merr != nil {
				continue
			}
			underlyingCache[cacheKey] = m
		}
		if !m.Subscribed || m.Symbol == "" {
			continue
		}
		key := m.Symbol + "|" + parts.Expiry.Format("2006-01-02")
		g := groups[key]
		if g == nil {
			g = &chainGroup{symbol: m.Symbol, expiry: parts.Expiry}
			groups[key] = g
		}
		g.members = append(g.members, heldOption{occ: parts, occID: p.InstrumentID, portfolioID: p.PortfolioID})
	}

	published := 0
	for _, g := range groups {
		chain, ferr := fetch(ctx, g.symbol, g.expiry)
		if ferr != nil {
			log.DefaultLogger.Warn("option poll: fetch failed", "symbol", g.symbol, "err", ferr)
			continue
		}
		if chain == nil || chain.MarketState != "REGULAR" {
			continue // market-hours gate
		}
		observedUs := chain.QuoteTimeUs
		if observedUs <= 0 {
			observedUs = nowMicros()
		}
		for _, mem := range g.members {
			row, ok := matchRow(chain.Rows, mem.occ.Strike, mem.occ.Right)
			if !ok {
				continue
			}
			mark, ok := markFromRow(row)
			if !ok {
				continue
			}
			ccy := firstNonEmpty(row.Currency, chain.UnderlyingCurrency, "USD")
			if err := publishOptionMark(ctx, client, pluginID, mem.occID, mem.portfolioID, mark, ccy, observedUs); err != nil {
				log.DefaultLogger.Warn("option poll: publish failed", "occ", mem.occID, "err", err)
				continue
			}
			published++
		}
	}
	return published
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

func atoiDefault(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

// readOptionPollSettings reads enable + interval from app_settings, defaulting
// to enabled / 900s when unset or unparseable.
func readOptionPollSettings(ctx context.Context, client rwPGClient) (bool, int) {
	enable, interval := true, 900
	res, err := client.PGQuery(ctx,
		`SELECT key, value FROM basic_data.app_settings WHERE key IN ('option_poll_enable','option_poll_interval_sec')`)
	if err != nil {
		return enable, interval
	}
	for _, row := range res.Rows {
		if len(row) < 2 {
			continue
		}
		k := rwString(row[0])
		v := rwString(row[1])
		switch k {
		case "option_poll_enable":
			enable = v != "false"
		case "option_poll_interval_sec":
			if n := atoiDefault(v, 900); n > 0 {
				interval = n
			}
		}
	}
	return enable, interval
}

// StartOptionPollLoop runs runOptionPollOnce on the configured interval. The
// loop re-reads settings each cycle so changes take effect without restart.
func StartOptionPollLoop(ctx context.Context, client rwPGClient, yf *YfClient, pluginID string) context.CancelFunc {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		fetch := func(c context.Context, symbol string, expiry time.Time) (*OptionChainResult, error) {
			return yf.FetchOptionChain(c, symbol, expiry)
		}
		for {
			enable, interval := readOptionPollSettings(ctx, client)
			if enable {
				n := runOptionPollOnce(ctx, client, pluginID, fetch)
				log.DefaultLogger.Debug("option poll tick", "published", n)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(interval) * time.Second):
			}
		}
	}()
	return cancel
}
