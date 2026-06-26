// pkg/plugin/handlers_overview_test.go
package plugin

import "testing"

func TestOverviewFromPairs(t *testing.T) {
	pairs := []heldPair{
		{PortfolioID: "pf1", InstrumentID: "AAPL", Kind: "equity"},
		{PortfolioID: "pf1", InstrumentID: "AAPL 17JAN25 150 C", Kind: "option"},
		{PortfolioID: "pf1", InstrumentID: "AAPL 17JAN25 160 C", Kind: "option"},
		{PortfolioID: "pf2", InstrumentID: "MSFT 17JAN25 400 P", Kind: "option"},
		// fx_pair and cash must NOT inflate the equity count
		{PortfolioID: "pf1", InstrumentID: "EURUSD", Kind: "fx_pair"},
		{PortfolioID: "pf1", InstrumentID: "USD", Kind: "cash"},
	}
	eq, opt, und := overviewFromPairs(pairs)
	if eq != 1 || opt != 3 || und != 2 {
		t.Fatalf("got eq=%d opt=%d und=%d, want 1/3/2 (fx_pair/cash must not count as equities)", eq, opt, und)
	}
}
