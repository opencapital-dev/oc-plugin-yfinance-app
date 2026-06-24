package plugin

import (
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
