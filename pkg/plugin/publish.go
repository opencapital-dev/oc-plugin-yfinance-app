package plugin

// Namespace constants. The gateway sees `/v6/data/<plugin_id>/<namespace>`
// and routes to data.v2 with these strings in source_namespace. Downstream
// RW MVs (`prices`, `instrument_per_event`, `portfolio_per_tick`) project
// the strings verbatim — keep them byte-identical to pre-v6.
const (
	OhlcvNamespace      = "prices.ohlcv"
	QuoteNamespace      = "prices.quote"
	OptionMarkNamespace = "prices.option_mark"
)
