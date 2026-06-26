package plugin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
)

type settingsPayload struct {
	FredAPIKey            *string  `json:"fred_api_key,omitempty"`
	PollIntervalSec       *int     `json:"pollIntervalSec,omitempty"`
	QPS                   *float64 `json:"qps,omitempty"`
	Burst                 *int     `json:"burst,omitempty"`
	LiveEnable            *bool    `json:"liveEnable,omitempty"`
	BackfillEnable        *bool    `json:"backfillEnable,omitempty"`
	OptionPollEnable      *bool    `json:"optionPollEnable,omitempty"`
	OptionPollIntervalSec *int     `json:"optionPollIntervalSec,omitempty"`
}

type optionPollBody struct {
	Enable      bool
	IntervalSec int
}

func optionPollSettings(ctx context.Context, client rwPGClient) optionPollBody {
	enable, interval := readOptionPollSettings(ctx, client)
	return optionPollBody{Enable: enable, IntervalSec: interval}
}

func (a *App) handleSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		res, err := a.client.PGQuery(ctx,
			`SELECT value FROM basic_data.app_settings WHERE key = $1`, "fred_api_key")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		set := len(res.Rows) > 0 && res.Rows[0][0] != nil && res.Rows[0][0] != ""
		op := optionPollSettings(ctx, a.client)
		writeJSON(w, map[string]any{
			"fred_api_key_set":      set,
			"pollIntervalSec":       a.options.DiscoveryPollSec,
			"qps":                   a.options.YfinanceQPS,
			"burst":                 a.options.YfinanceBurst,
			"liveEnable":            a.options.LiveEnable,
			"backfillEnable":        a.options.BackfillEnable,
			"optionPollEnable":      op.Enable,
			"optionPollIntervalSec": op.IntervalSec,
		})
	case http.MethodPut:
		var p settingsPayload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if p.FredAPIKey != nil {
			if _, err := a.client.PGExec(ctx,
				`INSERT INTO basic_data.app_settings (key, value, updated_at)
				 VALUES ($1, $2, now())
				 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
				"fred_api_key", *p.FredAPIKey); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		if p.OptionPollEnable != nil {
			val := "true"
			if !*p.OptionPollEnable {
				val = "false"
			}
			if _, err := a.client.PGExec(ctx,
				`INSERT INTO basic_data.app_settings (key, value, updated_at) VALUES ($1, $2, now())
				 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
				"option_poll_enable", val); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		if p.OptionPollIntervalSec != nil {
			if _, err := a.client.PGExec(ctx,
				`INSERT INTO basic_data.app_settings (key, value, updated_at) VALUES ($1, $2, now())
				 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
				"option_poll_interval_sec", strconv.Itoa(*p.OptionPollIntervalSec)); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		// poll/qps/burst/toggles persist to jsonData via the existing config path;
		// here we only persist the FRED key and option-poll settings. Echo success.
		writeJSON(w, map[string]any{"ok": true})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// handleTestFred verifies the stored key against a trivial FRED endpoint.
func (a *App) handleTestFred(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	res, err := a.client.PGQuery(ctx,
		`SELECT value FROM basic_data.app_settings WHERE key = $1`, "fred_api_key")
	if err != nil || len(res.Rows) == 0 || res.Rows[0][0] == nil {
		writeJSON(w, map[string]any{"ok": false, "error": "no key set"})
		return
	}
	key, _ := res.Rows[0][0].(string)
	url := "https://api.stlouisfed.org/fred/series?series_id=GDP&api_key=" + key + "&file_type=json"
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	writeJSON(w, map[string]any{"ok": resp.StatusCode == 200, "status": resp.StatusCode})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
