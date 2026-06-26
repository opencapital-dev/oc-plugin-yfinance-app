// pkg/plugin/option_publish_test.go
package plugin

import (
	"context"
	"strings"
	"testing"

	"github.com/opencapital-dev/oc-plugin-sdk/datakey"
	"github.com/opencapital-dev/oc-plugin-sdk/pluginclient"
)

// capClient captures the last Exec for assertions; other methods are no-ops.
type capClient struct {
	lastSQL  string
	lastArgs []any
}

func (c *capClient) Exec(_ context.Context, sql string, args ...any) (int64, error) {
	c.lastSQL, c.lastArgs = sql, args
	return 1, nil
}
func (c *capClient) Query(context.Context, string, ...any) (pluginclient.Result, error) {
	return pluginclient.Result{}, nil
}
func (c *capClient) PGExec(context.Context, string, ...any) (int64, error) { return 0, nil }
func (c *capClient) PGQuery(context.Context, string, ...any) (pluginclient.Result, error) {
	return pluginclient.Result{}, nil
}
func (c *capClient) Config() pluginclient.Config {
	return pluginclient.Config{PluginID: "basic-data-app"}
}

func TestPublishOptionMark(t *testing.T) {
	const (
		occID       = "AAPL 17JAN25 150 C"
		portfolioID = "pf1"
		pluginID    = "basic-data-app"
		observedUs  = int64(1_700_000_000_000_000)
	)
	c := &capClient{}
	err := publishOptionMark(context.Background(), c, pluginID, occID, portfolioID, 2.2, "USD", observedUs)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(c.lastSQL, "INSERT INTO data_log") {
		t.Fatalf("sql missing insert: %q", c.lastSQL)
	}
	if c.lastArgs[0] != OptionMarkNamespace {
		t.Errorf("arg0 = %v, want %v", c.lastArgs[0], OptionMarkNamespace)
	}
	if c.lastArgs[1] != occID || c.lastArgs[2] != portfolioID {
		t.Errorf("source_id/portfolio args wrong: %v %v", c.lastArgs[1], c.lastArgs[2])
	}
	payload, _ := c.lastArgs[7].(string)
	if !strings.Contains(payload, `"close":2.2`) || !strings.Contains(payload, `"currency":"USD"`) {
		t.Errorf("payload wrong: %s", payload)
	}
	// Assert rw_key arg (index 8) is computed correctly.
	wantRwKey := datakey.DataKey(pluginID, OptionMarkNamespace, portfolioID, occID, observedUs)
	if c.lastArgs[8] != wantRwKey {
		t.Errorf("rw_key arg[8] = %v, want %v", c.lastArgs[8], wantRwKey)
	}
}
