package plugin

import (
	"context"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
)

func StartDiscoveryLoop(
	ctx context.Context, client rwPGClient,
	app *App, state *BackfillState, liveSubs *LiveSubscriber,
	pollSec int,
) context.CancelFunc {
	if pollSec <= 0 {
		pollSec = 15
	}
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		seenMappingTs := map[string]int64{}
		seenSymbol := map[string]string{}
		lastEnqueuedStart := map[string]int64{}
		ticker := time.NewTicker(time.Duration(pollSec) * time.Second)
		defer ticker.Stop()
		discoveryTick(ctx, client, app, state, liveSubs, seenMappingTs, seenSymbol, lastEnqueuedStart)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				discoveryTick(ctx, client, app, state, liveSubs, seenMappingTs, seenSymbol, lastEnqueuedStart)
			}
		}
	}()
	return cancel
}

type heldPair struct {
	PortfolioID  string
	InstrumentID string
	FirstTs      time.Time
	Kind         string
	Currency     string
	BaseCurrency string
}

func heldPairs(ctx context.Context, client rwPGClient) ([]heldPair, error) {
	res, err := client.Query(ctx,
		`SELECT portfolio_id, instrument_id, kind, currency, base_currency, first_seen_ts FROM instruments_catalog`)
	if err != nil {
		return nil, err
	}
	col := colIndex(res.Columns)
	out := make([]heldPair, 0, len(res.Rows))
	for _, row := range res.Rows {
		us := rwMicros(row[col["first_seen_ts"]])
		out = append(out, heldPair{
			PortfolioID:  rwString(row[col["portfolio_id"]]),
			InstrumentID: rwString(row[col["instrument_id"]]),
			FirstTs:      time.UnixMicro(us).UTC(),
			Kind:         rwString(row[col["kind"]]),
			Currency:     rwString(row[col["currency"]]),
			BaseCurrency: rwString(row[col["base_currency"]]),
		})
	}
	return out, nil
}

func discoveryTick(
	ctx context.Context, client rwPGClient,
	app *App, state *BackfillState, liveSubs *LiveSubscriber,
	seenMappingTs map[string]int64, seenSymbol map[string]string,
	lastEnqueuedStart map[string]int64,
) {
	subs, err := app.ListSubscribedTickerMappings(ctx)
	if err != nil {
		log.DefaultLogger.Warn("discovery: list subscribed mappings failed", "err", err)
		return
	}

	mappingByPair := make(map[string]TickerMapping, len(subs))
	for _, m := range subs {
		mappingByPair[pairKey(m.InstrumentID, m.PortfolioID)] = m
	}

	if liveSubs != nil {
		liveSubs.SetSymbols(ctx, subs)
	}

	pairs, err := heldPairs(ctx, client)
	if err != nil {
		log.DefaultLogger.Warn("discovery: heldPairs query failed", "err", err)
		return
	}
	log.DefaultLogger.Debug("discovery tick", "held_pairs", len(pairs), "mapped", len(mappingByPair))
	if len(pairs) == 0 {
		return
	}

	endUs := nowMicros()

	for _, pair := range pairs {
		k := pairKey(pair.InstrumentID, pair.PortfolioID)
		m, hasMapped := mappingByPair[k]
		if !hasMapped || m.Symbol == "" {
			log.DefaultLogger.Debug("discovery skip: no symbol mapped",
				"instrument_id", pair.InstrumentID, "portfolio_id", pair.PortfolioID,
				"has_mapping", hasMapped)
			continue
		}

		firstTsUs := pair.FirstTs.UnixMicro()

		mts := int64(0)
		if m.UpdatedAt != nil {
			mts = *m.UpdatedAt
		}
		oldTs := seenMappingTs[k]
		oldSym := seenSymbol[k]
		prevStart, hasPrev := lastEnqueuedStart[k]

		mappingChanged := mts != oldTs || m.Symbol != oldSym
		windowGrew := hasPrev && prevStart > firstTsUs
		firstTime := !hasPrev

		if !(mappingChanged || windowGrew || firstTime) {
			continue
		}

		log.DefaultLogger.Info("discovery enqueue backfill",
			"instrument_id", pair.InstrumentID, "portfolio_id", pair.PortfolioID, "symbol", m.Symbol)
		state.Enqueue(&BackfillJob{
			JobID:        genID(),
			InstrumentID: pair.InstrumentID,
			PortfolioID:  pair.PortfolioID,
			BarSize:      "1d",
			StartTsUs:    firstTsUs,
			EndTsUs:      endUs,
			Origin:       "discovery",
			EnqueuedAtUs: nowMicros(),
		})
		seenMappingTs[k] = mts
		seenSymbol[k] = m.Symbol
		lastEnqueuedStart[k] = firstTsUs
	}
}
