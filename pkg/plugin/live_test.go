package plugin

import (
	"strings"
	"testing"
	"time"

	"github.com/opencapital-dev/oc-plugin-sdk/datakey"
	yfmodels "github.com/wnjoon/go-yfinance/pkg/models"
)

func TestQuoteObservedMicros(t *testing.T) {
	cases := []struct {
		name   string
		timeMs int64
		now    time.Time
		check  func(t *testing.T, result int64, timeMs int64, now time.Time)
	}{
		{
			name:   "2024-12-05 ms value converts to microseconds",
			timeMs: 1_733_400_000_000,
			now:    time.Time{},
			check: func(t *testing.T, result int64, timeMs int64, _ time.Time) {
				if result != timeMs*1_000 {
					t.Fatalf("result = %d, want %d (input*1000)", result, timeMs*1_000)
				}
				gotYear := time.UnixMicro(result).UTC().Year()
				wantYear := time.UnixMilli(timeMs).UTC().Year()
				if gotYear != wantYear {
					t.Fatalf("year = %d, want %d", gotYear, wantYear)
				}
			},
		},
		{
			name:   "zero timeMs falls back to now",
			timeMs: 0,
			now:    time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC),
			check: func(t *testing.T, result int64, _ int64, now time.Time) {
				if result != now.UnixMicro() {
					t.Fatalf("result = %d, want %d (now.UnixMicro)", result, now.UnixMicro())
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := quoteObservedMicros(tc.timeMs, tc.now)
			tc.check(t, result, tc.timeMs, tc.now)
		})
	}
}

// TestPublishTickDataLogInsert drives publishTick through the fakeClient seam
// and asserts: correct column order in the INSERT, namespace == prices.quote,
// source == "yahoo_ws", and rw_key == datakey.DataKey(...).
func TestPublishTickDataLogInsert(t *testing.T) {
	const pluginID = "yfinance-app"
	const portfolioID = "port-1"
	const instrumentID = "instr-1"
	const symbol = "AAPL"

	fc := &fakeClient{}
	sub := &LiveSubscriber{
		client:   fc,
		pluginID: pluginID,
		current:  map[string]struct{}{},
		bySymbol: map[string][]symbolTarget{
			symbol: {{InstrumentID: instrumentID, PortfolioID: portfolioID}},
		},
		ticks: NewLiveTickMap(),
	}

	// Use a known ms timestamp so we can compute the expected observedAtUs.
	const timeMs = int64(1_733_400_000_000)
	observedAtUs := timeMs * 1_000
	wantRwKey := datakey.DataKey(pluginID, QuoteNamespace, portfolioID, instrumentID, observedAtUs)

	data := &yfmodels.PricingData{
		ID:       symbol,
		Time:     timeMs,
		Price:    float32(150.0),
		Bid:      float32(149.9),
		Ask:      float32(150.1),
		BidSize:  100,
		AskSize:  100,
		Currency: "USD",
		Exchange: "XNAS",
	}
	sub.publishTick(data)

	if len(fc.execCalls) != 1 {
		t.Fatalf("expected 1 Exec call, got %d", len(fc.execCalls))
	}
	call := fc.execCalls[0]

	// Verify INSERT targets data_log.
	if !strings.Contains(call.sql, "INSERT INTO data_log") {
		t.Errorf("SQL missing INSERT INTO data_log: %s", call.sql)
	}

	args := call.args
	// Column order: source_namespace, source_id, portfolio_id, observedAtUs, source, plugin_id, trace_id, payload, rw_key
	if args[0] != QuoteNamespace {
		t.Errorf("arg[0] source_namespace = %v, want %v", args[0], QuoteNamespace)
	}
	if args[1] != instrumentID {
		t.Errorf("arg[1] source_id = %v, want %v", args[1], instrumentID)
	}
	if args[2] != portfolioID {
		t.Errorf("arg[2] portfolio_id = %v, want %v", args[2], portfolioID)
	}
	if args[3] != observedAtUs {
		t.Errorf("arg[3] observed_at_us = %v, want %v", args[3], observedAtUs)
	}
	if args[4] != "yahoo_ws" {
		t.Errorf("arg[4] source = %v, want yahoo_ws", args[4])
	}
	if args[5] != pluginID {
		t.Errorf("arg[5] plugin_id = %v, want %v", args[5], pluginID)
	}
	if args[8] != wantRwKey {
		t.Errorf("arg[8] rw_key = %v, want %v", args[8], wantRwKey)
	}
}

func TestCanonicalSymbol(t *testing.T) {
	// No canonical → raw symbol.
	if got := canonicalSymbol(TickerMapping{Symbol: "AET", VendorMeta: map[string]any{}}); got != "AET" {
		t.Errorf("no-canonical = %q, want AET", got)
	}
	// Canonical present → canonical wins.
	m := TickerMapping{Symbol: "AET", VendorMeta: map[string]any{
		"canonical": map[string]any{"symbol": "AET.L", "exch": "LSE"},
	}}
	if got := canonicalSymbol(m); got != "AET.L" {
		t.Errorf("canonical = %q, want AET.L", got)
	}
	// Empty canonical symbol → fall back to raw.
	m2 := TickerMapping{Symbol: "AET", VendorMeta: map[string]any{
		"canonical": map[string]any{"symbol": "", "exch": "LSE"},
	}}
	if got := canonicalSymbol(m2); got != "AET" {
		t.Errorf("empty-canonical = %q, want AET", got)
	}
}

func TestSetSymbolsUsesCanonical(t *testing.T) {
	mappings := []TickerMapping{
		{InstrumentID: "AET", PortfolioID: "p", Symbol: "AET",
			VendorMeta: map[string]any{"canonical": map[string]any{"symbol": "AET.L", "exch": "LSE"}}},
	}
	got := desiredSymbols(mappings)
	if _, ok := got["AET.L"]; !ok {
		t.Errorf("expected desired to contain canonical AET.L, got %v", got)
	}
	if _, ok := got["AET"]; ok {
		t.Errorf("must not subscribe the raw ambiguous symbol AET")
	}
}
