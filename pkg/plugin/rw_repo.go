package plugin

import (
	"context"
	"fmt"
)

type RwFxPairUsedRow struct {
	BaseCcy     string `json:"base_ccy"`
	QuoteCcy    string `json:"quote_ccy"`
	FirstSeenTs int64  `json:"first_seen_ts"`
	LastSeenTs  int64  `json:"last_seen_ts"`
	EventCount  int    `json:"event_count"`
}

// PurgeInstrumentPrices removes every price row — backfilled OHLCV bars AND
// live quotes — for one (portfolio, instrument). Called on a symbol remap:
// the instrument_id now points at a different Yahoo symbol, so all prior
// prices (possibly a different currency/scale, e.g. a stray TWD quote on a
// GBP equity) are stale. One scoped DELETE over pgwire; the next discovery
// tick re-backfills OHLCV and the live subscription repopulates quotes.
func (a *App) PurgeInstrumentPrices(ctx context.Context, instrumentID, portfolioID string) error {
	_, err := a.client.Exec(ctx,
		`DELETE FROM data_log
		   WHERE source_namespace IN ('prices.ohlcv', 'prices.quote')
		     AND source_id = $1 AND portfolio_id = $2`,
		instrumentID, portfolioID)
	if err != nil {
		return fmt.Errorf("purge instrument prices: %w", err)
	}
	return nil
}

// PortfolioNames maps portfolio_id → display name (from the CDC-mirrored
// `portfolios.attributes->>'name'`). Used to label portfolios by name instead
// of UUID in the UI. RW down / missing name → caller falls back to the id.
func (a *App) PortfolioNames(ctx context.Context) (map[string]string, error) {
	res, err := a.client.Query(ctx,
		`SELECT portfolio_id, attributes->>'name' AS name FROM portfolios`)
	if err != nil {
		return nil, fmt.Errorf("portfolio names: %w", err)
	}
	col := colIndex(res.Columns)
	out := make(map[string]string, len(res.Rows))
	for _, row := range res.Rows {
		id := rwString(row[col["portfolio_id"]])
		name := rwString(row[col["name"]])
		if id != "" && name != "" {
			out[id] = name
		}
	}
	return out, nil
}

func (a *App) ListFxPairsUsed(ctx context.Context) ([]RwFxPairUsedRow, error) {
	res, err := a.client.Query(ctx,
		`SELECT base_ccy, quote_ccy, first_seen_ts, last_seen_ts, event_count FROM fx_pairs_used`)
	if err != nil {
		return nil, fmt.Errorf("list fx_pairs_used: %w", err)
	}
	col := colIndex(res.Columns)
	out := make([]RwFxPairUsedRow, 0, len(res.Rows))
	for _, row := range res.Rows {
		out = append(out, RwFxPairUsedRow{
			BaseCcy:     rwString(row[col["base_ccy"]]),
			QuoteCcy:    rwString(row[col["quote_ccy"]]),
			FirstSeenTs: rwMicros(row[col["first_seen_ts"]]),
			LastSeenTs:  rwMicros(row[col["last_seen_ts"]]),
			EventCount:  rwInt(row[col["event_count"]]),
		})
	}
	return out, nil
}

func (a *App) LastObservedPerInstrument(ctx context.Context) (map[string]int64, error) {
	res, err := a.client.Query(ctx,
		`SELECT source_id, observed_at FROM ohlcv_coverage`)
	if err != nil {
		return nil, fmt.Errorf("last observed: %w", err)
	}
	col := colIndex(res.Columns)
	out := map[string]int64{}
	for _, row := range res.Rows {
		sid := rwString(row[col["source_id"]])
		ts := rwMicros(row[col["observed_at"]])
		if existing, ok := out[sid]; !ok || ts > existing {
			out[sid] = ts
		}
	}
	return out, nil
}

func (a *App) LastDataPerInstrument(ctx context.Context) (map[string]int64, error) {
	res, err := a.client.Query(ctx,
		`SELECT source_id, observed_at FROM data_coverage`)
	if err != nil {
		return nil, fmt.Errorf("last data: %w", err)
	}
	col := colIndex(res.Columns)
	out := map[string]int64{}
	for _, row := range res.Rows {
		sid := rwString(row[col["source_id"]])
		ts := rwMicros(row[col["observed_at"]])
		if existing, ok := out[sid]; !ok || ts > existing {
			out[sid] = ts
		}
	}
	return out, nil
}

func (a *App) MinBusinessTs(ctx context.Context) (*int64, error) {
	res, err := a.client.Query(ctx,
		`SELECT portfolio_id, instrument_id, first_seen_ts FROM instruments_catalog`)
	if err != nil {
		return nil, fmt.Errorf("min business_ts: %w", err)
	}
	col := colIndex(res.Columns)
	var min *int64
	for _, row := range res.Rows {
		us := rwMicros(row[col["first_seen_ts"]])
		if us == 0 {
			continue
		}
		if min == nil || us < *min {
			v := us
			min = &v
		}
	}
	return min, nil
}
