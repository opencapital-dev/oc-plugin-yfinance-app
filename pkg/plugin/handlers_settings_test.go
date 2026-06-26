package plugin

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/opencapital-dev/oc-plugin-sdk/pluginclient"
)

func TestPutSettingsUpsertsFredKey(t *testing.T) {
	fc := &fakeClient{}
	app := makeAppWithFakeClient(fc)
	req := httptest.NewRequest("PUT", "/settings", strings.NewReader(`{"fred_api_key":"abc123"}`))
	rec := httptest.NewRecorder()
	app.handleSettings(rec, req)
	if rec.Code != 200 {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var upserted bool
	for _, c := range fc.pgExecCalls {
		if strings.Contains(c.sql, "basic_data.app_settings") && strings.Contains(c.sql, "ON CONFLICT") {
			upserted = true
			if len(c.args) < 2 || c.args[0] != "fred_api_key" || c.args[1] != "abc123" {
				t.Errorf("unexpected upsert args: %v", c.args)
			}
		}
	}
	if !upserted {
		t.Error("expected an upsert into basic_data.app_settings")
	}
}

func TestGetSettingsReportsKeyPresenceNotValue(t *testing.T) {
	fc := &fakeClient{
		pgQueryResult: pluginclient.Result{
			Columns: []pluginclient.Column{{Name: "value"}},
			Rows:    [][]any{{"secret"}},
		},
	}
	app := makeAppWithFakeClient(fc)
	req := httptest.NewRequest("GET", "/settings", nil)
	rec := httptest.NewRecorder()
	app.handleSettings(rec, req)
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["fred_api_key_set"] != true {
		t.Errorf("want fred_api_key_set true, got %v", body["fred_api_key_set"])
	}
	if _, leaked := body["fred_api_key"]; leaked {
		t.Error("must not return the key value")
	}
}

func TestTestFredRejectsNonPost(t *testing.T) {
	app := makeAppWithFakeClient(&fakeClient{})
	req := httptest.NewRequest("GET", "/settings/test-fred", nil)
	rec := httptest.NewRecorder()
	app.handleTestFred(rec, req)
	if rec.Code != 405 {
		t.Fatalf("want 405, got %d", rec.Code)
	}
}

type settingsClient struct{ execs []string }

func (c *settingsClient) Exec(context.Context, string, ...any) (int64, error) { return 0, nil }
func (c *settingsClient) Query(context.Context, string, ...any) (pluginclient.Result, error) {
	return pluginclient.Result{}, nil
}
func (c *settingsClient) PGExec(_ context.Context, sql string, args ...any) (int64, error) {
	c.execs = append(c.execs, sql)
	return 1, nil
}
func (c *settingsClient) PGQuery(_ context.Context, sql string, args ...any) (pluginclient.Result, error) {
	// option_poll keys lookup → return enable=false, interval=600
	if strings.Contains(sql, "option_poll") {
		return pluginclient.Result{
			Columns: []pluginclient.Column{{Name: "key"}, {Name: "value"}},
			Rows:    [][]any{{"option_poll_enable", "false"}, {"option_poll_interval_sec", "600"}},
		}, nil
	}
	return pluginclient.Result{}, nil // fred key absent
}
func (c *settingsClient) Config() pluginclient.Config { return pluginclient.Config{} }

func TestSettingsGetIncludesOptionPoll(t *testing.T) {
	a := &App{client: &settingsClient{}, options: AppOptions{DiscoveryPollSec: 15, YfinanceQPS: 1, YfinanceBurst: 3}}
	body := optionPollSettings(context.Background(), a.client) // helper under test
	if body.Enable != false || body.IntervalSec != 600 {
		t.Fatalf("got %+v, want enable=false interval=600", body)
	}
}
