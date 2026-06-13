package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend/log"

	"github.com/ignacioballester/oc-plugin-sdk/pluginclient"
)

// StartBackfillWorker runs the single backfill worker goroutine. Jobs are
// pushed by the /yf/jobs/enqueue handler and by the discovery loop. The
// worker always tombstones every existing prices.ohlcv row for the
// (instrument, portfolio) pair before publishing the fresh set (catches
// pre-cutover orphans + across-restart symbol changes).
//
// `ctx` must carry a pluginclient identity (discovery loop attaches it).
// The worker re-uses that same identity for every gateway publish; the
// session JWT auto-refreshes inside pluginclient's session cache.
func StartBackfillWorker(
	ctx context.Context, state *BackfillState, yf *YfClient,
	client *pluginclient.Client, idleSleep time.Duration,
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
			runBackfillJob(ctx, state, yf, client, job)
		}
	}()
	return cancel
}

func runBackfillJob(
	ctx context.Context, state *BackfillState, yf *YfClient,
	client *pluginclient.Client, job *BackfillJob,
) {
	state.MarkRunning(job, nowMicros())

	// Mint a fresh identity for this job: OpenDB reads plugin/org from the ctx
	// identity and PublishData reads the bearer off it. WithRequest is cached +
	// refreshes near expiry, so long-running backfills never use a stale JWT.
	ctx, err := client.WithRequest(ctx, nil)
	if err != nil {
		log.DefaultLogger.Warn("backfill: mint identity failed",
			"instrument_id", job.InstrumentID, "portfolio_id", job.PortfolioID, "err", err)
		state.MarkFinished(job, "failed", -1, fmt.Sprintf("mint identity: %v", err), nowMicros())
		return
	}

	// Resolve the Yahoo symbol from per-(plugin, org) SQLite keyed by
	// (instrument_id, portfolio_id).
	db, err := client.OpenDB(ctx, migrationsFS)
	if err != nil {
		state.MarkFinished(job, "failed", -1, fmt.Sprintf("open sqlite: %v", err), nowMicros())
		return
	}
	var symbol string
	if err := db.QueryRowContext(ctx,
		`SELECT symbol FROM instrument_ticker_mapping WHERE instrument_id = ? AND portfolio_id = ?`,
		job.InstrumentID, job.PortfolioID,
	).Scan(&symbol); err != nil {
		log.DefaultLogger.Warn("backfill: resolve symbol failed",
			"instrument_id", job.InstrumentID, "portfolio_id", job.PortfolioID, "err", err)
		state.MarkFinished(job, "failed", -1, fmt.Sprintf("resolve symbol: %v", err), nowMicros())
		return
	}

	// Purge every pre-existing bar for this (instrument, portfolio) pair
	// before publishing the fresh set, read via read-gateway. Best-effort —
	// a transient failure should not block the backfill. The query is
	// instrument-scoped (no portfolio matcher, so no ownership round-trip);
	// portfolio_id is filtered from the returned column.
	res, qerr := client.ReadGatewayQuery(ctx, pluginclient.ReadGatewayRequest{
		Bindings:   []pluginclient.ReadGatewayBinding{{Name: "A", Selector: fmt.Sprintf(`ohlcv_coverage{instrument=%q} @window`, job.InstrumentID)}},
		OutputMode: "table",
		To:         nowMicros(),
	})
	if qerr == nil {
		col := colIndex(res.Columns)
		keys := make([]pluginclient.TombstoneKey, 0, len(res.Rows))
		for _, row := range res.Rows {
			if asString(row[col["portfolio_id"]]) != job.PortfolioID {
				continue
			}
			keys = append(keys, pluginclient.TombstoneKey{
				SourceID:    job.InstrumentID,
				ObservedAt:  asMicros(row[col["observed_at"]]),
				PortfolioID: job.PortfolioID,
			})
		}
		if len(keys) > 0 {
			if terr := client.PublishTombstones(ctx, OhlcvNamespace, keys); terr != nil {
				log.DefaultLogger.Warn("ohlcv tombstone failed",
					"instrument_id", job.InstrumentID,
					"portfolio_id", job.PortfolioID,
					"tombstoned", len(keys), "err", terr)
			} else {
				log.DefaultLogger.Info("ohlcv purge",
					"instrument_id", job.InstrumentID,
					"portfolio_id", job.PortfolioID,
					"tombstoned", len(keys))
			}
		}
	} else {
		log.DefaultLogger.Warn("ohlcv key read failed; skipping purge",
			"instrument_id", job.InstrumentID,
			"portfolio_id", job.PortfolioID,
			"err", qerr)
	}

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
			continue // unparsed/garbage bar date — don't publish a year-0002 observation
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
		body := map[string]any{
			"source_id":    job.InstrumentID,
			"observed_at":  bar.Date.UnixMicro(),
			"portfolio_id": job.PortfolioID,
			"payload":      string(payloadJSON),
		}
		if _, err := client.PublishData(ctx, OhlcvNamespace, body); err != nil {
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
