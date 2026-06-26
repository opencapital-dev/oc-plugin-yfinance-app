package plugin

import "net/http"

// registerRoutes wires every CallResource path the yfinance plugin serves.
// v6: every handler runs locally against pluginclient (SQLite + gateway +
// RW pool); no Postgres or Kafka surface remains.
func (a *App) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/yf/lookup", a.handleLookup)
	mux.HandleFunc("/yf/instruments", a.handleListInstruments)
	mux.HandleFunc("/yf/fx-pairs", a.handleListFxPairs)
	mux.HandleFunc("/yf/jobs", a.handleListJobs)
	mux.HandleFunc("/yf/jobs/enqueue", a.handleEnqueueJob)
	mux.HandleFunc("/yf/symbols/", a.handleSymbol)
	mux.HandleFunc("/yf/classification/", a.handleClassification)
	mux.HandleFunc("/settings", a.handleSettings)
	mux.HandleFunc("/settings/test-fred", a.handleTestFred)
	mux.HandleFunc("/yf/option-underlyings", a.handleOptionUnderlyings)
	mux.HandleFunc("/yf/overview", a.handleOverview)
}
