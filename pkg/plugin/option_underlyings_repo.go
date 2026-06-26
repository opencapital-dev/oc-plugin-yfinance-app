// pkg/plugin/option_underlyings_repo.go
package plugin

import "context"

type underlyingMapping struct {
	Symbol     string
	Subscribed bool
}

// optionUnderlyingKey returns the namespaced instrument_id used for option-underlying
// rows in instrument_ticker_mapping. The "@opt:" prefix cannot collide with any
// OCC root (which matches [A-Z][A-Z0-9.]*), preventing equity row corruption.
func optionUnderlyingKey(root string) string { return "@opt:" + root }

// resolveOptionUnderlying returns the option_underlying mapping for
// (root, portfolioID). On first sight (no row) it seeds a default
// symbol=root, subscribed=true row and returns it. The row is stored under
// optionUnderlyingKey(root) so it never collides with an equity row.
func resolveOptionUnderlying(ctx context.Context, client rwPGClient, root, portfolioID string) (underlyingMapping, error) {
	key := optionUnderlyingKey(root)
	res, err := client.PGQuery(ctx,
		`SELECT symbol, subscribed FROM basic_data.instrument_ticker_mapping
		 WHERE instrument_id = $1 AND portfolio_id = $2`, key, portfolioID)
	if err != nil {
		return underlyingMapping{}, err
	}
	if len(res.Rows) > 0 {
		col := colIndex(res.Columns)
		sub := true
		if b, ok := res.Rows[0][col["subscribed"]].(bool); ok {
			sub = b
		}
		return underlyingMapping{Symbol: rwString(res.Rows[0][col["symbol"]]), Subscribed: sub}, nil
	}
	now := nowMicros()
	if _, err := client.PGExec(ctx,
		`INSERT INTO basic_data.instrument_ticker_mapping
			(instrument_id, portfolio_id, symbol, vendor_meta, subscribed, created_at, updated_at, updated_by)
		 VALUES ($1, $2, $3, '{"kind":"option_underlying"}'::jsonb, TRUE, $4, $4, 'option-poll')
		 ON CONFLICT (instrument_id, portfolio_id) DO NOTHING`,
		key, portfolioID, root, now); err != nil {
		return underlyingMapping{}, err
	}
	return underlyingMapping{Symbol: root, Subscribed: true}, nil
}

// lookupOptionUnderlying returns the option_underlying mapping for
// (root, portfolioID) without any write side-effects. When the row is absent
// it returns a default {Symbol: root, Subscribed: true} — the same default
// as resolveOptionUnderlying — but does NOT insert anything.
func lookupOptionUnderlying(ctx context.Context, client rwPGClient, root, portfolioID string) (underlyingMapping, error) {
	key := optionUnderlyingKey(root)
	res, err := client.PGQuery(ctx,
		`SELECT symbol, subscribed FROM basic_data.instrument_ticker_mapping
		 WHERE instrument_id = $1 AND portfolio_id = $2`, key, portfolioID)
	if err != nil {
		return underlyingMapping{}, err
	}
	if len(res.Rows) > 0 {
		col := colIndex(res.Columns)
		sub := true
		if b, ok := res.Rows[0][col["subscribed"]].(bool); ok {
			sub = b
		}
		return underlyingMapping{Symbol: rwString(res.Rows[0][col["symbol"]]), Subscribed: sub}, nil
	}
	// No row yet — return safe default without seeding.
	return underlyingMapping{Symbol: root, Subscribed: true}, nil
}
