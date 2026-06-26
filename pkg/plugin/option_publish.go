// pkg/plugin/option_publish.go
package plugin

import (
	"context"
	"encoding/json"

	"github.com/opencapital-dev/oc-plugin-sdk/datakey"
)

// publishOptionMark writes one prices.option_mark row to data_log. mark is the
// per-share option premium (contract_multiplier applies downstream). observedUs
// is the mark observation time in unix micros.
func publishOptionMark(ctx context.Context, client rwPGClient, pluginID, occID, portfolioID string, mark float64, currency string, observedUs int64) error {
	payloadJSON, err := json.Marshal(map[string]any{"close": mark, "currency": currency})
	if err != nil {
		return err
	}
	rwKey := datakey.DataKey(pluginID, OptionMarkNamespace, portfolioID, occID, observedUs)
	_, err = client.Exec(ctx, `
		INSERT INTO data_log
			(source_namespace, source_id, portfolio_id, observed_at, ingest_ts, source, plugin_id, trace_id, payload, rw_key)
		VALUES ($1, $2, $3, to_timestamp($4::double precision / 1e6), now(), $5, $6, $7, $8, $9)
	`,
		OptionMarkNamespace, occID, portfolioID, observedUs,
		"yahoo_options", pluginID, "", string(payloadJSON), rwKey,
	)
	return err
}
