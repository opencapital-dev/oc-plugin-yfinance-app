package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/opencapital-dev/oc-plugin-sdk/datakey"
)

func StartBackfillWorker(
	ctx context.Context, state *BackfillState, yf *YfClient,
	client rwPGClient, app *App, idleSleep time.Duration,
) context.CancelFunc {
	if idleSleep <= 0 {
		idleSleep = 250 * time.Millisecond
	}
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		for {
			if ctx.Err() != nil {
				return
			}
			job, ok := state.Pop()
			if !ok {
				select {
				case <-ctx.Done():
					return
				case <-time.After(idleSleep):
					continue
				}
			}
			runBackfillJob(ctx, state, yf, client, app, job)
		}
	}()
	return cancel
}

func runBackfillJob(
	ctx context.Context, state *BackfillState, yf *YfClient,
	client rwPGClient, app *App, job *BackfillJob,
) {
	state.MarkRunning(job, nowMicros())

	mapping, err := app.GetTickerMapping(ctx, job.InstrumentID, job.PortfolioID)
	if err != nil {
		log.DefaultLogger.Warn("backfill: resolve symbol failed",
			"instrument_id", job.InstrumentID, "portfolio_id", job.PortfolioID, "err", err)
		state.MarkFinished(job, "failed", -1, fmt.Sprintf("resolve symbol: %v", err), nowMicros())
		return
	}
	symbol := mapping.Symbol

	// No pre-delete here: bars are upserted on rw_key (plugin|ns|portfolio|
	// instrument|observed_at), so re-running a backfill overwrites the same
	// timestamps idempotently and a grown window just inserts earlier bars.
	// Stale prices from a *symbol change* are purged at the remap site
	// (UpsertTickerMapping → PurgeInstrumentPrices), which also covers live
	// quotes — something this OHLCV worker must never delete.

	start := time.UnixMicro(job.StartTsUs).UTC()
	end := time.UnixMicro(job.EndTsUs).UTC()
	bars, rawCurrency, referencePrice, err := yf.FetchBars(ctx, symbol, job.BarSize, start, end)
	if err != nil {
		log.DefaultLogger.Warn("backfill: fetch bars failed",
			"instrument_id", job.InstrumentID, "portfolio_id", job.PortfolioID,
			"symbol", symbol, "err", err)
		state.MarkFinished(job, "failed", -1, err.Error(), nowMicros())
		return
	}

	majorCurrency, divisor := normalizeMinorUnits(rawCurrency)
	barCadence := pythonBarCadence(job.BarSize)

	published := 0
	for _, bar := range bars {
		if bar.Date.IsZero() || bar.Date.Year() < 1970 {
			continue
		}
		open, high, low, closeP := bar.Open, bar.High, bar.Low, bar.Close
		if divisor > 1.0 && classifyBarUnit(closeP, referencePrice, divisor) == "minor" {
			open /= divisor
			high /= divisor
			low /= divisor
			closeP /= divisor
		}
		if open <= 0 || high <= 0 || low <= 0 || closeP <= 0 {
			continue
		}
		volume := float64(bar.Volume)
		if volume < 0 {
			volume = 0
		}
		payloadJSON, perr := json.Marshal(map[string]any{
			"open":        open,
			"high":        high,
			"low":         low,
			"close":       closeP,
			"volume":      volume,
			"trade_count": nil,
			"bar_cadence": barCadence,
			"currency":    majorCurrency,
			"venue":       "YAHOO",
		})
		if perr != nil {
			continue
		}
		observedAtUs := bar.Date.UnixMicro()
		rwKey := datakey.DataKey(app.pluginID, OhlcvNamespace, job.PortfolioID, job.InstrumentID, observedAtUs)
		_, err := client.Exec(ctx, `
			INSERT INTO data_log
				(source_namespace, source_id, portfolio_id, observed_at, ingest_ts, source, plugin_id, trace_id, payload, rw_key)
			VALUES ($1, $2, $3, to_timestamp($4::double precision / 1e6), now(), $5, $6, $7, $8, $9)
		`,
			OhlcvNamespace, job.InstrumentID, job.PortfolioID, observedAtUs,
			"yfinance", app.pluginID, "", string(payloadJSON), rwKey,
		)
		if err != nil {
			log.DefaultLogger.Warn("ohlcv publish failed",
				"instrument_id", job.InstrumentID,
				"portfolio_id", job.PortfolioID,
				"err", err)
			continue
		}
		published++
	}

	state.MarkFinished(job, "done", published, "", nowMicros())
	log.DefaultLogger.Info("backfill done",
		"instrument_id", job.InstrumentID,
		"portfolio_id", job.PortfolioID,
		"symbol", symbol,
		"yahoo_currency", rawCurrency, "currency", majorCurrency,
		"divisor", divisor, "bars", published)
}

func pythonBarCadence(barSize string) string {
	switch barSize {
	case "1m", "5m", "15m", "30m":
		return "1m"
	case "60m", "90m", "1h":
		return "1h"
	default:
		return "1d"
	}
}
