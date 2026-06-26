// pkg/plugin/handlers_option_underlyings_test.go
package plugin

import (
	"testing"
)

func TestRollupHeldContracts(t *testing.T) {
	// pure helper: count held option contracts per (root, portfolio)
	pairs := []heldPair{
		{PortfolioID: "pf1", InstrumentID: "AAPL 17JAN25 150 C", Kind: "option"},
		{PortfolioID: "pf1", InstrumentID: "AAPL 17JAN25 160 C", Kind: "option"},
		{PortfolioID: "pf1", InstrumentID: "MSFT 17JAN25 400 P", Kind: "option"},
		{PortfolioID: "pf1", InstrumentID: "AAPL", Kind: "equity"},
	}
	got := heldContractsByRoot(pairs)
	if got["AAPL|pf1"] != 2 || got["MSFT|pf1"] != 1 {
		t.Fatalf("got %v, want AAPL|pf1=2 MSFT|pf1=1", got)
	}
	if _, ok := got["AAPL|pf1"]; !ok {
		t.Fatal("missing AAPL")
	}
}
