// pkg/plugin/optionchain_test.go
package plugin

import (
	"testing"

	yfmodels "github.com/wnjoon/go-yfinance/pkg/models"
)

func TestMarkFromRowMidWhenBothQuotes(t *testing.T) {
	mark, ok := markFromRow(OptionRow{Bid: 2.0, Ask: 2.4, LastPrice: 9})
	if !ok || mark != 2.2 {
		t.Fatalf("mark=%v ok=%v, want 2.2 true", mark, ok)
	}
}

func TestMarkFromRowFallsBackToLast(t *testing.T) {
	mark, ok := markFromRow(OptionRow{Bid: 0, Ask: 0, LastPrice: 3.1})
	if !ok || mark != 3.1 {
		t.Fatalf("mark=%v ok=%v, want 3.1 true", mark, ok)
	}
}

func TestMarkFromRowSkipsWhenNoPrice(t *testing.T) {
	if _, ok := markFromRow(OptionRow{Bid: 0, Ask: 0, LastPrice: 0}); ok {
		t.Fatal("expected ok=false when no usable price")
	}
}

func TestOptionResultFromChainPopulatesUnderlying(t *testing.T) {
	chain := &yfmodels.OptionChain{
		Calls:      []yfmodels.Option{{Strike: 150, Bid: 1, Ask: 2}},
		Underlying: &yfmodels.OptionQuote{MarketState: "REGULAR", Currency: "USD", RegularMarketTime: 1700},
	}
	res := optionResultFromChain(chain)
	if res.MarketState != "REGULAR" || res.UnderlyingCurrency != "USD" {
		t.Fatalf("underlying not mapped: %+v", res)
	}
	if res.QuoteTimeUs != 1700*1_000_000 {
		t.Fatalf("QuoteTimeUs = %d, want %d", res.QuoteTimeUs, 1700*1_000_000)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(res.Rows))
	}
}

func TestMatchRowByStrikeAndRight(t *testing.T) {
	rows := optionRowsFromChain(
		[]yfmodels.Option{{Strike: 150, Bid: 1, Ask: 2, Currency: "USD"}},
		[]yfmodels.Option{{Strike: 150, Bid: 3, Ask: 4}},
	)
	got, ok := matchRow(rows, 150, "P")
	if !ok || got.Bid != 3 {
		t.Fatalf("got %+v ok=%v, want put bid 3", got, ok)
	}
	c, ok := matchRow(rows, 150, "C")
	if !ok || c.Bid != 1 {
		t.Fatalf("got %+v ok=%v, want call bid 1", c, ok)
	}
	if _, ok := matchRow(rows, 999, "C"); ok {
		t.Fatal("expected no match for absent strike")
	}
}
