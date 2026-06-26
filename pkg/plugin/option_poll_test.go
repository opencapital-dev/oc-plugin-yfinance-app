// pkg/plugin/option_poll_test.go
package plugin

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/opencapital-dev/oc-plugin-sdk/pluginclient"
)

// futureOccID builds an OCC-format instrument_id one year from now so the
// expired-guard in runOptionPollOnce never skips the fixture.
func futureOccID() string {
	d := time.Now().AddDate(1, 0, 0)
	return fmt.Sprintf("AAPL %02d%s%02d 150 C", d.Day(), strings.ToUpper(d.Month().String()[:3]), d.Year()%100)
}

// pollClient: Query returns one held option; PGQuery returns a subscribed
// underlying mapping; Exec captures publishes.
type pollClient struct {
	published    int
	instrumentID string // set per-test to a future OCC id
}

func (c *pollClient) Exec(context.Context, string, ...any) (int64, error) {
	c.published++
	return 1, nil
}
func (c *pollClient) Query(context.Context, string, ...any) (pluginclient.Result, error) {
	id := c.instrumentID
	if id == "" {
		id = futureOccID()
	}
	return pluginclient.Result{
		Columns: []pluginclient.Column{{Name: "portfolio_id"}, {Name: "instrument_id"}, {Name: "kind"}, {Name: "currency"}, {Name: "base_currency"}, {Name: "first_seen_ts"}},
		Rows:    [][]any{{"pf1", id, "option", "USD", "USD", int64(0)}},
	}, nil
}
func (c *pollClient) PGExec(context.Context, string, ...any) (int64, error) { return 0, nil }
func (c *pollClient) PGQuery(context.Context, string, ...any) (pluginclient.Result, error) {
	return pluginclient.Result{
		Columns: []pluginclient.Column{{Name: "symbol"}, {Name: "subscribed"}},
		Rows:    [][]any{{"AAPL", true}},
	}, nil
}
func (c *pollClient) Config() pluginclient.Config { return pluginclient.Config{} }

// chainStub returns a one-call-one-strike chain. Strike 150 / right C matches
// the held fixture built by futureOccID().
func chainStub(market string) chainFetchFn {
	return func(_ context.Context, _ string, _ time.Time) (*OptionChainResult, error) {
		return &OptionChainResult{
			MarketState: market, UnderlyingCurrency: "USD", QuoteTimeUs: 1_700_000_000_000_000,
			Rows: []OptionRow{{Strike: 150, Right: "C", Bid: 2.0, Ask: 2.4}},
		}, nil
	}
}

func TestRunOptionPollOncePublishesMatchedMark(t *testing.T) {
	c := &pollClient{instrumentID: futureOccID()}
	n := runOptionPollOnce(context.Background(), c, "basic-data-app", chainStub("REGULAR"))
	if n != 1 || c.published != 1 {
		t.Fatalf("published n=%d exec=%d, want 1/1", n, c.published)
	}
}

func TestRunOptionPollOnceSkipsWhenMarketClosed(t *testing.T) {
	c := &pollClient{instrumentID: futureOccID()}
	n := runOptionPollOnce(context.Background(), c, "basic-data-app", chainStub("CLOSED"))
	if n != 0 || c.published != 0 {
		t.Fatalf("published n=%d exec=%d, want 0/0 when closed", n, c.published)
	}
}
