package plugin

import (
	"context"
	"fmt"

	"github.com/ignacioballester/oc-plugin-sdk/pluginclient"
)

// RwFxPairUsedRow is the per-pair row from RisingWave's `fx_pairs_used`
// MV — the handler-side type FxPairUsedRow is composed from this + the
// matching control-plane instrument projection (fetched separately).
type RwFxPairUsedRow struct {
	BaseCcy     string `json:"base_ccy"`
	QuoteCcy    string `json:"quote_ccy"`
	FirstSeenTs int64  `json:"first_seen_ts"`
	LastSeenTs  int64  `json:"last_seen_ts"`
	EventCount  int    `json:"event_count"`
}

func (a *App) ListFxPairsUsed(ctx context.Context) ([]RwFxPairUsedRow, error) {
	res, err := a.client.ReadGatewayQuery(ctx, pluginclient.ReadGatewayRequest{
		Bindings:   []pluginclient.ReadGatewayBinding{{Name: "A", Selector: `fx_pairs_used{} @latest`}},
		OutputMode: "table",
		To:         nowMicros(),
	})
	if err != nil {
		return nil, fmt.Errorf("list fx_pairs_used: %w", err)
	}
	col := colIndex(res.Columns)
	out := make([]RwFxPairUsedRow, 0, len(res.Rows))
	for _, row := range res.Rows {
		out = append(out, RwFxPairUsedRow{
			BaseCcy:     asString(row[col["base_ccy"]]),
			QuoteCcy:    asString(row[col["quote_ccy"]]),
			FirstSeenTs: asMicros(row[col["first_seen_ts"]]),
			LastSeenTs:  asMicros(row[col["last_seen_ts"]]),
			EventCount:  asInt(row[col["event_count"]]),
		})
	}
	return out, nil
}

// LastObservedPerInstrument returns the newest observed_at per instrument the
// plugin has published under prices.ohlcv — the data-coverage column on the
// /yf/instruments page. @latest over ohlcv_coverage's source_id grain yields
// one row per instrument with its most-recent observed_at.
func (a *App) LastObservedPerInstrument(ctx context.Context) (map[string]int64, error) {
	res, err := a.client.ReadGatewayQuery(ctx, pluginclient.ReadGatewayRequest{
		Bindings:   []pluginclient.ReadGatewayBinding{{Name: "A", Selector: `ohlcv_coverage{} @latest`}},
		OutputMode: "table",
		To:         nowMicros(),
	})
	if err != nil {
		return nil, fmt.Errorf("last observed: %w", err)
	}
	col := colIndex(res.Columns)
	out := map[string]int64{}
	for _, row := range res.Rows {
		out[asString(row[col["source_id"]])] = asMicros(row[col["observed_at"]])
	}
	return out, nil
}

// LastDataPerInstrument returns the newest observed_at per instrument across
// ALL of the plugin's published namespaces (live quotes + backfill bars) — the
// "Last data" column on /yf/instruments. @latest over data_coverage's source_id
// grain yields one row per instrument with its most-recent data point,
// regardless of which namespace produced it.
func (a *App) LastDataPerInstrument(ctx context.Context) (map[string]int64, error) {
	res, err := a.client.ReadGatewayQuery(ctx, pluginclient.ReadGatewayRequest{
		Bindings:   []pluginclient.ReadGatewayBinding{{Name: "A", Selector: `data_coverage{} @latest`}},
		OutputMode: "table",
		To:         nowMicros(),
	})
	if err != nil {
		return nil, fmt.Errorf("last data: %w", err)
	}
	col := colIndex(res.Columns)
	out := map[string]int64{}
	for _, row := range res.Rows {
		out[asString(row[col["source_id"]])] = asMicros(row[col["observed_at"]])
	}
	return out, nil
}

// OhlcvKeysForInstrument returns every observed_at the plugin previously
// published under prices.ohlcv for instrumentID. Used by the
// always-purge-before-backfill flow.
func (a *App) OhlcvKeysForInstrument(ctx context.Context, instrumentID string) ([]int64, error) {
	res, err := a.client.ReadGatewayQuery(ctx, pluginclient.ReadGatewayRequest{
		Bindings:   []pluginclient.ReadGatewayBinding{{Name: "A", Selector: fmt.Sprintf(`ohlcv_coverage{instrument=%q} @window`, instrumentID)}},
		OutputMode: "table",
		To:         nowMicros(),
	})
	if err != nil {
		return nil, fmt.Errorf("ohlcv keys: %w", err)
	}
	col := colIndex(res.Columns)
	out := make([]int64, 0, len(res.Rows))
	for _, row := range res.Rows {
		out = append(out, asMicros(row[col["observed_at"]]))
	}
	return out, nil
}

// MinBusinessTs is the lower bound of the planned backfill window. Derived as
// the earliest first_seen_ts across the org's held instruments (each is the
// MIN(business_ts) for that instrument). nil when no instruments exist yet.
func (a *App) MinBusinessTs(ctx context.Context) (*int64, error) {
	res, err := a.client.ReadGatewayQuery(ctx, pluginclient.ReadGatewayRequest{
		Bindings:   []pluginclient.ReadGatewayBinding{{Name: "A", Selector: `instruments_used{} @latest`}},
		OutputMode: "table",
		To:         nowMicros(),
	})
	if err != nil {
		return nil, fmt.Errorf("min business_ts: %w", err)
	}
	col := colIndex(res.Columns)
	var min *int64
	for _, row := range res.Rows {
		us := asMicros(row[col["first_seen_ts"]])
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
