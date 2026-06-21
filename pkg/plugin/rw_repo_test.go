package plugin

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/opencapital-dev/oc-plugin-sdk/datakey"
)

func TestOhlcvDataLogInsert(t *testing.T) {
	fc := &fakeClient{}
	app := &App{client: fc, pluginID: "yfinance-app"}
	ctx := context.Background()

	instrumentID := "instr-1"
	portfolioID := "port-1"
	observedAtUs := int64(1_700_000_000_000_000)
	wantRwKey := datakey.DataKey("yfinance-app", OhlcvNamespace, portfolioID, instrumentID, observedAtUs)

	_, err := app.client.Exec(ctx, `
		INSERT INTO data_log
			(source_namespace, source_id, portfolio_id, observed_at, ingest_ts, source, plugin_id, trace_id, payload, rw_key)
		VALUES ($1, $2, $3, to_timestamp($4::double precision / 1e6), now(), $5, $6, $7, $8, $9)
	`,
		OhlcvNamespace, instrumentID, portfolioID, observedAtUs,
		"yfinance", app.pluginID, "", `{"open":100}`, wantRwKey,
	)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if len(fc.execCalls) != 1 {
		t.Fatalf("expected 1 Exec call, got %d", len(fc.execCalls))
	}
	args := fc.execCalls[0].args
	if args[0] != OhlcvNamespace {
		t.Errorf("arg[0] source_namespace = %v, want %v", args[0], OhlcvNamespace)
	}
	if args[8] != wantRwKey {
		t.Errorf("arg[8] rw_key = %v, want %v", args[8], wantRwKey)
	}
}

func TestPurgeInstrumentPrices(t *testing.T) {
	fc := &fakeClient{}
	app := &App{client: fc, pluginID: "yfinance-app"}
	ctx := context.Background()

	if err := app.PurgeInstrumentPrices(ctx, "instr-1", "port-1"); err != nil {
		t.Fatalf("PurgeInstrumentPrices: %v", err)
	}
	if len(fc.execCalls) != 1 {
		t.Fatalf("expected 1 Exec (purge), got %d", len(fc.execCalls))
	}
	sql := fc.execCalls[0].sql
	if !strings.Contains(sql, "DELETE FROM data_log") {
		t.Errorf("SQL not a DELETE on data_log: %s", sql)
	}
	// Both price namespaces must be purged — quotes were the leftover that
	// the old ohlcv-only purge missed.
	if !strings.Contains(sql, "prices.ohlcv") || !strings.Contains(sql, "prices.quote") {
		t.Errorf("purge must cover both price namespaces: %s", sql)
	}
	args := fc.execCalls[0].args
	if args[0] != "instr-1" || args[1] != "port-1" {
		t.Errorf("purge args = %v, want [instr-1 port-1]", args)
	}
}

func TestRwHelpers(t *testing.T) {
	t.Run("rwMicros from int64", func(t *testing.T) {
		if v := rwMicros(int64(1234)); v != 1234 {
			t.Errorf("rwMicros(int64) = %d, want 1234", v)
		}
	})
	t.Run("rwMicros from int32", func(t *testing.T) {
		if v := rwMicros(int32(999)); v != 999 {
			t.Errorf("rwMicros(int32) = %d, want 999", v)
		}
	})
	t.Run("rwMicros from time.Time", func(t *testing.T) {
		ts := time.Date(2025, 1, 2, 3, 4, 5, 6000, time.UTC)
		want := ts.UnixMicro()
		if v := rwMicros(ts); v != want {
			t.Errorf("rwMicros(time.Time) = %d, want %d", v, want)
		}
	})
	t.Run("rwString nil", func(t *testing.T) {
		if v := rwString(nil); v != "" {
			t.Errorf("rwString(nil) = %q, want empty", v)
		}
	})
}
