// pkg/plugin/handlers_option_underlyings.go
package plugin

import (
	"encoding/json"
	"net/http"
)

type optionUnderlyingRow struct {
	Root          string `json:"root"`
	PortfolioID   string `json:"portfolio_id"`
	Symbol        string `json:"symbol"`
	Subscribed    bool   `json:"subscribed"`
	HeldContracts int    `json:"held_contracts"`
}

type optionUnderlyingUpsert struct {
	Root        string  `json:"root"`
	PortfolioID string  `json:"portfolio_id"`
	Symbol      *string `json:"symbol,omitempty"`
	Subscribed  *bool   `json:"subscribed,omitempty"`
}

// heldContractsByRoot counts held option contracts keyed "root|portfolio".
func heldContractsByRoot(pairs []heldPair) map[string]int {
	out := map[string]int{}
	for _, p := range pairs {
		if p.Kind != "option" {
			continue
		}
		parts, err := ParseOcc(p.InstrumentID)
		if err != nil {
			continue
		}
		out[parts.Underlying+"|"+p.PortfolioID]++
	}
	return out
}

func (a *App) handleOptionUnderlyings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		pairs, err := heldPairs(ctx, a.client)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		counts := heldContractsByRoot(pairs)
		rows := make([]optionUnderlyingRow, 0, len(counts))
		for key, n := range counts {
			root, pf := splitKey(key)
			m, err := lookupOptionUnderlying(ctx, a.client, root, pf)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			rows = append(rows, optionUnderlyingRow{
				Root: root, PortfolioID: pf, Symbol: m.Symbol,
				Subscribed: m.Subscribed, HeldContracts: n,
			})
		}
		writeJSON(w, rows)
	case http.MethodPost:
		var p optionUnderlyingUpsert
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if p.Root == "" || p.PortfolioID == "" {
			http.Error(w, "root and portfolio_id required", http.StatusBadRequest)
			return
		}
		// Ensure a row exists under the namespaced key, then patch provided fields.
		optKey := optionUnderlyingKey(p.Root)
		if _, err := resolveOptionUnderlying(ctx, a.client, p.Root, p.PortfolioID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		now := nowMicros()
		if p.Symbol != nil {
			if _, err := a.client.PGExec(ctx,
				`UPDATE basic_data.instrument_ticker_mapping SET symbol = $3, updated_at = $4, updated_by = 'options-tab'
				 WHERE instrument_id = $1 AND portfolio_id = $2`,
				optKey, p.PortfolioID, *p.Symbol, now); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		if p.Subscribed != nil {
			if _, err := a.client.PGExec(ctx,
				`UPDATE basic_data.instrument_ticker_mapping SET subscribed = $3, updated_at = $4, updated_by = 'options-tab'
				 WHERE instrument_id = $1 AND portfolio_id = $2`,
				optKey, p.PortfolioID, *p.Subscribed, now); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		writeJSON(w, map[string]any{"ok": true})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func splitKey(k string) (string, string) {
	for i := 0; i < len(k); i++ {
		if k[i] == '|' {
			return k[:i], k[i+1:]
		}
	}
	return k, ""
}
