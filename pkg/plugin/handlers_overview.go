// pkg/plugin/handlers_overview.go
package plugin

import "net/http"

type overviewBody struct {
	HeldEquities      int   `json:"held_equities"`
	HeldOptions       int   `json:"held_options"`
	OptionUnderlyings int   `json:"option_underlyings"`
	LastOptionMarkUs  int64 `json:"last_option_mark_us"`
}

func overviewFromPairs(pairs []heldPair) (equities, options, underlyings int) {
	roots := map[string]struct{}{}
	for _, p := range pairs {
		if p.Kind == "option" {
			options++
			if parts, err := ParseOcc(p.InstrumentID); err == nil {
				roots[parts.Underlying+"|"+p.PortfolioID] = struct{}{}
			}
			continue
		}
		if p.Kind == "equity" {
			equities++
		}
	}
	return equities, options, len(roots)
}

func (a *App) handleOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	pairs, err := heldPairs(ctx, a.client)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	eq, opt, und := overviewFromPairs(pairs)
	body := overviewBody{HeldEquities: eq, HeldOptions: opt, OptionUnderlyings: und}

	res, err := a.client.Query(ctx,
		`SELECT max(observed_at) FROM data_log WHERE source_namespace = $1`, OptionMarkNamespace)
	if err == nil && len(res.Rows) > 0 {
		body.LastOptionMarkUs = rwMicros(res.Rows[0][0])
	}
	writeJSON(w, body)
}
