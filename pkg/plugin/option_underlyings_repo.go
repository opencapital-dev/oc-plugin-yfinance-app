// pkg/plugin/option_underlyings_repo.go
package plugin

import "context"

type underlyingMapping struct {
	Symbol     string
	Subscribed bool
}

// resolveOptionUnderlying returns the option_underlying mapping for
// (root, portfolioID). On first sight (no row) it seeds a default
// symbol=root, subscribed=true row and returns it.
func resolveOptionUnderlying(ctx context.Context, client rwPGClient, root, portfolioID string) (underlyingMapping, error) {
	res, err := client.PGQuery(ctx,
		`SELECT symbol, subscribed FROM basic_data.instrument_ticker_mapping
		 WHERE instrument_id = $1 AND portfolio_id = $2`, root, portfolioID)
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
		root, portfolioID, root, now); err != nil {
		return underlyingMapping{}, err
	}
	return underlyingMapping{Symbol: root, Subscribed: true}, nil
}
