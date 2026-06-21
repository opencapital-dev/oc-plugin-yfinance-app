package plugin

import (
	"net/http"
)

// InstrumentUsedRow is one (instrument, portfolio) pair: the observed pair
// from RisingWave, overlaid with the operator's SQLite mapping + backfill
// state. last_observed_at / backfill_start_ts are per-instrument (not
// per-pair), so they may reflect any portfolio holding that instrument.
type InstrumentUsedRow struct {
	InstrumentID           string   `json:"instrument_id"`
	PortfolioID            string   `json:"portfolio_id"`
	PortfolioName          *string  `json:"portfolio_name"`
	Kind                   string   `json:"kind"`
	Currency               *string  `json:"currency"`
	BaseCurrency           *string  `json:"base_currency"`
	YahooSymbol            *string  `json:"yahoo_symbol"`
	Sector                 *string  `json:"sector"`
	Industry               *string  `json:"industry"`
	Subscribed             bool     `json:"subscribed"`
	BackfillStartTs        *int64   `json:"backfill_start_ts"`
	BackfillState          string   `json:"backfill_state"`
	LatestRunStatus        *string  `json:"latest_run_status"`
	LatestRunRowsPublished *int     `json:"latest_run_rows_published"`
	LatestRunError         *string  `json:"latest_run_error"`
	LatestRunFinishedAt    *int64   `json:"latest_run_finished_at"`
	LatestRunBarSize       *string  `json:"latest_run_bar_size"`
	LastObservedAt         *int64   `json:"last_observed_at"`
	LastTickAgeSec         *float64 `json:"last_tick_age_sec"`
	// LastDataAt is the most recent data point for the instrument — the newer
	// of the backfill coverage timestamp and the live-quote timestamp (epoch
	// micros). Populated whenever any data exists, so the UI always has a
	// "last data" recency, not just during market hours.
	LastDataAt *int64 `json:"last_data_at"`
}

func (a *App) handleListInstruments(w http.ResponseWriter, r *http.Request) {
	if !methodGuard(w, r, http.MethodGet) {
		return
	}
	ctx, ok := a.handlerCtx(w, r)
	if !ok {
		return
	}
	mappings, err := a.ListTickerMappings(ctx)
	if err != nil {
		respondErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	mapByPair := make(map[string]TickerMapping, len(mappings))
	for _, m := range mappings {
		mapByPair[pairKey(m.InstrumentID, m.PortfolioID)] = m
	}

	// Observed pairs come from imported events (RW); the mapping only adds the
	// operator's symbol/sector. RW down -> fall back to mapped pairs.
	pairs, _ := heldPairs(ctx, a.client)
	heldByPair := make(map[string]heldPair, len(pairs))
	for _, p := range pairs {
		heldByPair[pairKey(p.InstrumentID, p.PortfolioID)] = p
	}
	type instPair struct{ instrument, portfolio string }
	ordered := make([]instPair, 0, len(pairs)+len(mappings))
	seen := map[string]bool{}
	add := func(instrument, portfolio string) {
		k := pairKey(instrument, portfolio)
		if !seen[k] {
			seen[k] = true
			ordered = append(ordered, instPair{instrument, portfolio})
		}
	}
	for _, p := range pairs {
		add(p.InstrumentID, p.PortfolioID)
	}
	for _, m := range mappings {
		add(m.InstrumentID, m.PortfolioID)
	}

	portfolioNames, _ := a.PortfolioNames(ctx)
	minBusinessTs, _ := a.MinBusinessTs(ctx)
	lastObserved, _ := a.LastObservedPerInstrument(ctx)
	lastDataByInstrument, _ := a.LastDataPerInstrument(ctx)
	tickSnap := map[string]int64{}
	if a.ticks != nil {
		tickSnap = a.ticks.Snapshot()
	}
	nowUs := nowMicros()

	out := make([]InstrumentUsedRow, 0, len(ordered))
	for _, p := range ordered {
		m, mapped := mapByPair[pairKey(p.instrument, p.portfolio)]
		var sym *string
		if mapped && m.Symbol != "" {
			s := m.Symbol
			sym = &s
		}
		subscribed := true
		if v, ok := m.VendorMeta["subscribed"].(bool); ok {
			subscribed = v
		}
		bfState, runStatus, rowsPub, errMsg, finishedAt, barSize :=
			backfillSummaryFor(a.jobs, p.instrument, p.portfolio)
		var lastObs *int64
		if v, ok := lastObserved[p.instrument]; ok {
			lastObs = &v
		}
		var lastData *int64
		if v, ok := lastDataByInstrument[p.instrument]; ok {
			lastData = &v
		}
		var tickAge *float64
		if tickUs, ok := tickSnap[pairKey(p.instrument, p.portfolio)]; ok {
			age := float64(nowUs-tickUs) / 1_000_000
			tickAge = &age
		}
		hp := heldByPair[pairKey(p.instrument, p.portfolio)]
		var pname *string
		if n, ok := portfolioNames[p.portfolio]; ok && n != "" {
			pname = &n
		}
		out = append(out, InstrumentUsedRow{
			InstrumentID:           p.instrument,
			PortfolioID:            p.portfolio,
			PortfolioName:          pname,
			Kind:                   hp.Kind,
			Currency:               nilIfEmpty(hp.Currency),
			BaseCurrency:           nilIfEmpty(hp.BaseCurrency),
			YahooSymbol:            sym,
			Sector:                 m.Sector,
			Industry:               m.Subindustry,
			Subscribed:             subscribed,
			BackfillStartTs:        minBusinessTs,
			BackfillState:          bfState,
			LatestRunStatus:        runStatus,
			LatestRunRowsPublished: rowsPub,
			LatestRunError:         errMsg,
			LatestRunFinishedAt:    finishedAt,
			LatestRunBarSize:       barSize,
			LastObservedAt:         lastObs,
			LastTickAgeSec:         tickAge,
			LastDataAt:             lastData,
		})
	}
	respondJSON(w, http.StatusOK, out)
}

// backfillSummaryFor projects the in-memory job state for a single
// (instrument, portfolio) pair into the (state, status, rows, err,
// finished_at, bar_size) tuple the frontend renders.
func backfillSummaryFor(state *BackfillState, instrumentID, portfolioID string) (
	bfState string, runStatus *string, rowsPublished *int, errMsg *string,
	finishedAt *int64, barSize *string,
) {
	bfState = "none"
	if state == nil {
		return
	}
	if running := state.RunningFor(instrumentID, portfolioID); running != nil {
		bfState = "running"
	} else if pending := state.PendingFor(instrumentID, portfolioID); pending != nil {
		bfState = "pending"
	}
	last := state.LatestResultFor(instrumentID, portfolioID)
	if last == nil {
		return
	}
	if bfState == "none" {
		bfState = last.Status
	}
	st := last.Status
	runStatus = &st
	rowsPublished = last.RowsPublished
	errMsg = last.Error
	finishedAt = last.FinishedAtUs
	bs := last.BarSize
	barSize = &bs
	return
}
