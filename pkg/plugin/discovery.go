package plugin

import (
	"context"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend/log"

	"github.com/ignacioballester/oc-plugin-sdk/pluginclient"
)

// StartDiscoveryLoop polls the per-(plugin, org) SQLite ticker-mapping
// table every pollSec seconds and enqueues a backfill job whenever any of:
//   - the mapping for a (instrument, portfolio) pair is new this loop lifetime
//   - the operator-set symbol changed
//   - the per-pair earliest event_ts shrunk (newly-loaded portfolio history
//     pushes the lower bound back, so prior backfills no longer cover the range)
//
// Cold start (no portfolio events yet) skips enqueue entirely — the next
// tick discovers. The backfill worker always tombstones every existing
// prices.ohlcv row before publishing, so symbol changes are handled
// idempotently without depending on the discovery cursor.
//
// `ctx` carries the per-request identity from the call that triggered
// the lazy runtime start. The loop reuses that identity across every
// tick; pluginclient handles JWT + cert refresh internally.
func StartDiscoveryLoop(
	ctx context.Context, client *pluginclient.Client,
	state *BackfillState, liveSubs *LiveSubscriber,
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
		discoveryTick(ctx, client, state, liveSubs, seenMappingTs, seenSymbol, lastEnqueuedStart)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				discoveryTick(ctx, client, state, liveSubs, seenMappingTs, seenSymbol, lastEnqueuedStart)
			}
		}
	}()
	return cancel
}

// heldPair is one (portfolio, instrument) combination derived from the
// event-driven RW MV, together with the earliest trade timestamp so the
// backfill window starts exactly where the data does.
type heldPair struct {
	PortfolioID  string
	InstrumentID string
	FirstTs      time.Time
	Kind         string
	Currency     string
	BaseCurrency string
}

// heldPairs returns every (portfolio, instrument) pair with at least one event,
// with the earliest event timestamp as the backfill window lower bound. Reads
// the org-scoped instruments_used_v (the only portfolio-scoped instrument view
// exposed to the plugin schema).
func heldPairs(ctx context.Context, client *pluginclient.Client) ([]heldPair, error) {
	res, err := client.ReadGatewayQuery(ctx, pluginclient.ReadGatewayRequest{
		Bindings:   []pluginclient.ReadGatewayBinding{{Name: "A", Selector: `instruments_used{} @latest`}},
		OutputMode: "table",
		To:         nowMicros(),
	})
	if err != nil {
		return nil, err
	}
	col := colIndex(res.Columns)
	out := make([]heldPair, 0, len(res.Rows))
	for _, row := range res.Rows {
		out = append(out, heldPair{
			PortfolioID:  asString(row[col["portfolio_id"]]),
			InstrumentID: asString(row[col["instrument_id"]]),
			FirstTs:      time.UnixMicro(asMicros(row[col["first_seen_ts"]])).UTC(),
			Kind:         asString(row[col["kind"]]),
			Currency:     asString(row[col["currency"]]),
			BaseCurrency: asString(row[col["base_currency"]]),
		})
	}
	return out, nil
}

func discoveryTick(
	ctx context.Context, client *pluginclient.Client,
	state *BackfillState, liveSubs *LiveSubscriber,
	seenMappingTs map[string]int64, seenSymbol map[string]string,
	lastEnqueuedStart map[string]int64,
) {
	// Mint a fresh identity each tick: the gateway publish path (live SetSymbols
	// + backfill) reads the bearer off the ctx identity, and OpenDB reads the
	// plugin/org from it. WithRequest is cached + refreshes near expiry.
	ctx, err := client.WithRequest(ctx, nil)
	if err != nil {
		log.DefaultLogger.Warn("discovery: mint identity failed", "err", err)
		return
	}
	subs, err := listSubscribed(ctx, client)
	if err != nil {
		log.DefaultLogger.Warn("discovery: list subscribed mappings failed", "err", err)
		return
	}

	// Build a lookup map: pairKey → TickerMapping for fast per-pair resolution.
	mappingByPair := make(map[string]TickerMapping, len(subs))
	for _, m := range subs {
		mappingByPair[pairKey(m.InstrumentID, m.PortfolioID)] = m
	}

	// Always-refresh the live WS subscription set + publish ctx,
	// regardless of whether the backfill window is ready — markets stay
	// subscribable even before the first portfolio event lands.
	if liveSubs != nil {
		liveSubs.SetSymbols(ctx, subs)
	}

	// Query RW for held (portfolio, instrument) pairs and their first timestamp.
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

// listSubscribed reads every active mapping from the per-(plugin, org)
// SQLite. Filters muted rows server-side via the JSON_EXTRACT path.
func listSubscribed(ctx context.Context, client *pluginclient.Client) ([]TickerMapping, error) {
	db, err := client.OpenDB(ctx, migrationsFS)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `
		SELECT instrument_id, portfolio_id, symbol, sector, subindustry,
		       vendor_meta, created_at, updated_at, updated_by
		  FROM instrument_ticker_mapping
		 ORDER BY portfolio_id, instrument_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TickerMapping{}
	for rows.Next() {
		m, err := scanTickerMappingRow(rows)
		if err != nil {
			return nil, err
		}
		if sub, ok := m.VendorMeta["subscribed"].(bool); ok && !sub {
			continue
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
