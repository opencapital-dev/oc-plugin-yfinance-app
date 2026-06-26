// pkg/plugin/option_underlyings_repo_test.go
package plugin

import (
	"context"
	"strings"
	"testing"

	"github.com/opencapital-dev/oc-plugin-sdk/pluginclient"
)

// seedClient: PGQuery returns empty (no row) the first time, capturing the
// seed PGExec.
type seedClient struct {
	rows     [][]any
	seededID string
}

func (c *seedClient) Exec(context.Context, string, ...any) (int64, error) { return 0, nil }
func (c *seedClient) Query(context.Context, string, ...any) (pluginclient.Result, error) {
	return pluginclient.Result{}, nil
}
func (c *seedClient) PGExec(_ context.Context, sql string, args ...any) (int64, error) {
	if strings.Contains(sql, "INSERT INTO basic_data.instrument_ticker_mapping") {
		c.seededID, _ = args[0].(string)
	}
	return 1, nil
}
func (c *seedClient) PGQuery(context.Context, string, ...any) (pluginclient.Result, error) {
	return pluginclient.Result{Columns: []pluginclient.Column{{Name: "symbol"}, {Name: "subscribed"}}, Rows: c.rows}, nil
}
func (c *seedClient) Config() pluginclient.Config { return pluginclient.Config{} }

func TestResolveOptionUnderlyingSeedsWhenAbsent(t *testing.T) {
	c := &seedClient{rows: nil} // no existing row
	m, err := resolveOptionUnderlying(context.Background(), c, "AAPL", "pf1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if m.Symbol != "AAPL" || !m.Subscribed {
		t.Fatalf("got %+v, want AAPL/subscribed", m)
	}
	// The seeded instrument_id must use the namespaced key, not the bare root.
	if c.seededID != "@opt:AAPL" {
		t.Fatalf("seededID=%q, want @opt:AAPL", c.seededID)
	}
}

func TestResolveOptionUnderlyingUsesExisting(t *testing.T) {
	c := &seedClient{rows: [][]any{{"^SPX", true}}}
	m, err := resolveOptionUnderlying(context.Background(), c, "SPX", "pf1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if m.Symbol != "^SPX" || !m.Subscribed {
		t.Fatalf("got %+v, want ^SPX/subscribed", m)
	}
	if c.seededID != "" {
		t.Fatal("should not seed when row exists")
	}
}

// noExecSeedClient tracks PGExec calls; PGQuery always returns empty.
type noExecSeedClient struct {
	pgExecCount int
}

func (c *noExecSeedClient) Exec(context.Context, string, ...any) (int64, error) { return 0, nil }
func (c *noExecSeedClient) Query(context.Context, string, ...any) (pluginclient.Result, error) {
	return pluginclient.Result{}, nil
}
func (c *noExecSeedClient) PGExec(_ context.Context, _ string, _ ...any) (int64, error) {
	c.pgExecCount++
	return 1, nil
}
func (c *noExecSeedClient) PGQuery(context.Context, string, ...any) (pluginclient.Result, error) {
	return pluginclient.Result{Columns: []pluginclient.Column{{Name: "symbol"}, {Name: "subscribed"}}, Rows: nil}, nil
}
func (c *noExecSeedClient) Config() pluginclient.Config { return pluginclient.Config{} }

func TestLookupOptionUnderlyingNoPGExecWhenAbsent(t *testing.T) {
	c := &noExecSeedClient{}
	m, err := lookupOptionUnderlying(context.Background(), c, "AAPL", "pf1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Should return the default (symbol=root, subscribed=true) without inserting.
	if m.Symbol != "AAPL" || !m.Subscribed {
		t.Fatalf("got %+v, want AAPL/subscribed", m)
	}
	if c.pgExecCount != 0 {
		t.Fatalf("lookupOptionUnderlying issued %d PGExec calls, want 0", c.pgExecCount)
	}
}
