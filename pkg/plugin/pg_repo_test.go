package plugin

import (
	"context"
	"strings"
	"testing"

	"github.com/opencapital-dev/oc-plugin-sdk/pluginclient"
)

func makeAppWithFakeClient(fc *fakeClient) *App {
	return &App{
		client:   fc,
		pluginID: "test-plugin",
	}
}

func TestUpsertTickerMappingSQL(t *testing.T) {
	fc := &fakeClient{
		pgQueryResult: pluginclient.Result{
			Columns: []pluginclient.Column{
				{Name: "instrument_id"}, {Name: "portfolio_id"}, {Name: "symbol"},
				{Name: "sector"}, {Name: "subindustry"}, {Name: "vendor_meta"},
				{Name: "subscribed"}, {Name: "created_at"}, {Name: "updated_at"}, {Name: "updated_by"},
			},
			Rows: [][]any{
				{"instr-1", "port-1", "AAPL", nil, nil, map[string]interface{}{}, true, int64(1000), int64(1000), nil},
			},
		},
	}
	app := makeAppWithFakeClient(fc)
	ctx := context.Background()
	_, err := app.UpsertTickerMapping(ctx, "instr-1", "port-1", "AAPL", nil, "test")
	if err != nil {
		t.Fatalf("UpsertTickerMapping: %v", err)
	}
	if len(fc.pgExecCalls) != 1 {
		t.Fatalf("expected 1 PGExec call, got %d", len(fc.pgExecCalls))
	}
	sql := fc.pgExecCalls[0].sql
	if !strings.Contains(sql, "yfinance.instrument_ticker_mapping") {
		t.Errorf("SQL missing table name: %s", sql)
	}
	if !strings.Contains(sql, "ON CONFLICT") {
		t.Errorf("SQL missing ON CONFLICT: %s", sql)
	}
	args := fc.pgExecCalls[0].args
	if args[0] != "instr-1" {
		t.Errorf("arg[0] instrument_id = %v, want instr-1", args[0])
	}
	if args[1] != "port-1" {
		t.Errorf("arg[1] portfolio_id = %v, want port-1", args[1])
	}
	if args[2] != "AAPL" {
		t.Errorf("arg[2] symbol = %v, want AAPL", args[2])
	}
}

func mappingResult(symbol string) pluginclient.Result {
	return pluginclient.Result{
		Columns: []pluginclient.Column{
			{Name: "instrument_id"}, {Name: "portfolio_id"}, {Name: "symbol"},
			{Name: "sector"}, {Name: "subindustry"}, {Name: "vendor_meta"},
			{Name: "subscribed"}, {Name: "created_at"}, {Name: "updated_at"}, {Name: "updated_by"},
		},
		Rows: [][]any{
			{"instr-1", "port-1", symbol, nil, nil, map[string]interface{}{}, true, int64(1000), int64(1000), nil},
		},
	}
}

// A symbol remap points instrument_id at a different company, so every price
// previously written under it is stale — purge both backfilled bars and live
// quotes. This is the leftover-datapoint fix.
func TestUpsertTickerMappingPurgesPricesOnSymbolChange(t *testing.T) {
	fc := &fakeClient{pgQueryResult: mappingResult("OLD.L")}
	app := makeAppWithFakeClient(fc)
	_, err := app.UpsertTickerMapping(context.Background(), "instr-1", "port-1", "NEW", nil, "test")
	if err != nil {
		t.Fatalf("UpsertTickerMapping: %v", err)
	}
	if len(fc.execCalls) != 1 {
		t.Fatalf("expected 1 Exec (price purge), got %d", len(fc.execCalls))
	}
	sql := fc.execCalls[0].sql
	if !strings.Contains(sql, "DELETE FROM data_log") {
		t.Errorf("purge SQL not a DELETE on data_log: %s", sql)
	}
	if !strings.Contains(sql, "prices.ohlcv") || !strings.Contains(sql, "prices.quote") {
		t.Errorf("purge must cover both price namespaces: %s", sql)
	}
}

// Re-asserting the same symbol (idempotent POST, classification refresh) must
// not purge — that would drop live quotes for no reason.
func TestUpsertTickerMappingNoPurgeWhenSymbolUnchanged(t *testing.T) {
	fc := &fakeClient{pgQueryResult: mappingResult("AAPL")}
	app := makeAppWithFakeClient(fc)
	_, err := app.UpsertTickerMapping(context.Background(), "instr-1", "port-1", "AAPL", nil, "test")
	if err != nil {
		t.Fatalf("UpsertTickerMapping: %v", err)
	}
	if len(fc.execCalls) != 0 {
		t.Fatalf("expected no purge when symbol unchanged, got %d Exec calls", len(fc.execCalls))
	}
}

func TestGetTickerMappingNotFound(t *testing.T) {
	fc := &fakeClient{
		pgQueryResult: pluginclient.Result{Columns: []pluginclient.Column{{Name: "instrument_id"}}, Rows: nil},
	}
	app := makeAppWithFakeClient(fc)
	_, err := app.GetTickerMapping(context.Background(), "x", "y")
	if err != errNotFound {
		t.Fatalf("expected errNotFound, got %v", err)
	}
}

func TestEnsureSchema(t *testing.T) {
	fc := &fakeClient{}
	app := makeAppWithFakeClient(fc)
	app.ensureSchema(context.Background())

	if len(fc.pgExecCalls) < 2 {
		t.Fatalf("expected at least 2 PGExec calls (CREATE SCHEMA + CREATE TABLE ...), got %d", len(fc.pgExecCalls))
	}

	// First call must be CREATE SCHEMA IF NOT EXISTS yfinance.
	firstSQL := fc.pgExecCalls[0].sql
	if !strings.Contains(firstSQL, "CREATE SCHEMA IF NOT EXISTS yfinance") {
		t.Errorf("first PGExec call missing CREATE SCHEMA IF NOT EXISTS yfinance: %s", firstSQL)
	}

	// One of the calls must create the instrument_ticker_mapping table.
	foundTable := false
	for _, c := range fc.pgExecCalls {
		if strings.Contains(c.sql, "instrument_ticker_mapping") {
			foundTable = true
			break
		}
	}
	if !foundTable {
		t.Error("no PGExec call references instrument_ticker_mapping")
	}
}

func TestListSubscribedTickerMappingsSQL(t *testing.T) {
	fc := &fakeClient{
		pgQueryResult: pluginclient.Result{
			Columns: []pluginclient.Column{
				{Name: "instrument_id"}, {Name: "portfolio_id"}, {Name: "symbol"},
				{Name: "sector"}, {Name: "subindustry"}, {Name: "vendor_meta"},
				{Name: "subscribed"}, {Name: "created_at"}, {Name: "updated_at"}, {Name: "updated_by"},
			},
			Rows: [][]any{
				{"instr-1", "port-1", "AAPL", nil, nil, map[string]interface{}{}, true, int64(1000), int64(1000), nil},
			},
		},
	}
	app := makeAppWithFakeClient(fc)
	rows, err := app.ListSubscribedTickerMappings(context.Background())
	if err != nil {
		t.Fatalf("ListSubscribedTickerMappings: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	sql := fc.pgQueryCalls[0].sql
	if !strings.Contains(sql, "WHERE subscribed") {
		t.Errorf("SQL missing WHERE subscribed: %s", sql)
	}
}

func TestSetCanonicalIdentitySQL(t *testing.T) {
	fc := &fakeClient{pgQueryResult: mappingResult("AET.L")}
	app := makeAppWithFakeClient(fc)
	if err := app.SetCanonicalIdentity(context.Background(), "instr-1", "port-1", "AET.L", "LSE"); err != nil {
		t.Fatalf("SetCanonicalIdentity: %v", err)
	}
	if len(fc.pgExecCalls) != 1 {
		t.Fatalf("expected 1 PGExec, got %d", len(fc.pgExecCalls))
	}
	sql := fc.pgExecCalls[0].sql
	if !strings.Contains(sql, "instrument_ticker_mapping") || !strings.Contains(sql, "vendor_meta") {
		t.Errorf("SQL missing table/vendor_meta: %s", sql)
	}
	var found bool
	for _, a := range fc.pgExecCalls[0].args {
		if s, ok := a.(string); ok && strings.Contains(s, `"canonical"`) && strings.Contains(s, `"AET.L"`) {
			found = true
		}
	}
	if !found {
		t.Errorf("vendor_meta arg missing canonical AET.L: %v", fc.pgExecCalls[0].args)
	}
}

func TestSetCanonicalIdentityNoopOnEmpty(t *testing.T) {
	fc := &fakeClient{pgQueryResult: mappingResult("AET.L")}
	app := makeAppWithFakeClient(fc)
	if err := app.SetCanonicalIdentity(context.Background(), "instr-1", "port-1", "", "LSE"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(fc.pgExecCalls) != 0 {
		t.Fatalf("empty symbol must be a no-op, got %d PGExec", len(fc.pgExecCalls))
	}
}
