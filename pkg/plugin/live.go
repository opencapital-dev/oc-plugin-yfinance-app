package plugin

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	yflive "github.com/wnjoon/go-yfinance/pkg/live"
	yfmodels "github.com/wnjoon/go-yfinance/pkg/models"

	"github.com/ignacioballester/oc-plugin-sdk/pluginclient"
)

// symbolTarget is one (instrument, portfolio) pair that a Yahoo ticker backs.
// A single symbol can serve multiple portfolios (e.g. AAPL in portfolio-A and
// portfolio-B) — each gets its own published record with the correct portfolio_id.
type symbolTarget struct {
	InstrumentID string
	PortfolioID  string
}

// LiveSubscriber owns the WebSocket connection and the symbol→targets
// reverse map. The discovery loop calls SetSymbols(mappings) on each tick;
// LiveSubscriber diffs against its prior set and sends only
// Subscribe/Unsubscribe deltas to Yahoo.
//
// Each PricingData callback maps `data.ID` (a Yahoo ticker) back to the
// originating (instrument_id, portfolio_id) pairs via the reverse map, then
// publishes one prices.quote envelope per pair to data.v2 via pluginclient,
// and updates the LiveTickMap so the /yf/instruments endpoint can render
// "live" badges.
type LiveSubscriber struct {
	ws      *yflive.WebSocket
	client  *pluginclient.Client
	ticks   *LiveTickMap
	mu      sync.Mutex
	current map[string]struct{}
	// bySymbol maps upper(symbol) → []symbolTarget (one per (instrument, portfolio) pair)
	bySymbol map[string][]symbolTarget

	// publishCtx carries identity for asynchronous WebSocket callbacks
	// that arrive outside any HTTP request lifecycle. The discovery loop
	// refreshes this on every tick so the cached session JWT is fresh.
	publishCtx atomicCtx
}

// atomicCtx is the tiny sync wrapper around the publish ctx — multiple
// goroutines read while the discovery loop writes.
type atomicCtx struct {
	mu  sync.RWMutex
	ctx context.Context
}

func (a *atomicCtx) load() context.Context {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.ctx
}

func (a *atomicCtx) store(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ctx = ctx
}

func NewLiveSubscriber(client *pluginclient.Client, ticks *LiveTickMap) (*LiveSubscriber, error) {
	ws, err := yflive.New()
	if err != nil {
		return nil, err
	}
	return &LiveSubscriber{
		ws:       ws,
		client:   client,
		ticks:    ticks,
		current:  map[string]struct{}{},
		bySymbol: map[string][]symbolTarget{},
	}, nil
}

// Start connects + begins the listen goroutine.
func (s *LiveSubscriber) Start(_ context.Context) error {
	if err := s.ws.Connect(); err != nil {
		return err
	}
	_ = s.ws.ListenAsync(func(data *yfmodels.PricingData) {
		s.publishTick(data)
	})
	return nil
}

// SetSymbols replaces the subscription set + reverse map and refreshes
// the publish ctx (so the next tick's PublishData picks up a fresh
// identity-bearing context). The mappings slice is already the full
// subscribed set from discovery — one entry per (instrument, portfolio) pair.
func (s *LiveSubscriber) SetSymbols(ctx context.Context, mappings []TickerMapping) {
	desired := map[string]struct{}{}
	bySymbol := map[string][]symbolTarget{}
	for _, m := range mappings {
		if m.Symbol == "" {
			continue
		}
		up := strings.ToUpper(m.Symbol)
		desired[up] = struct{}{}
		bySymbol[up] = append(bySymbol[up], symbolTarget{
			InstrumentID: m.InstrumentID,
			PortfolioID:  m.PortfolioID,
		})
	}

	s.mu.Lock()
	toAdd := []string{}
	toRemove := []string{}
	for sym := range desired {
		if _, ok := s.current[sym]; !ok {
			toAdd = append(toAdd, sym)
		}
	}
	for sym := range s.current {
		if _, ok := desired[sym]; !ok {
			toRemove = append(toRemove, sym)
		}
	}
	s.current = desired
	s.bySymbol = bySymbol
	s.mu.Unlock()
	s.publishCtx.store(ctx)

	sort.Strings(toAdd)
	sort.Strings(toRemove)

	if len(toAdd) > 0 {
		if err := s.ws.Subscribe(toAdd); err != nil {
			log.DefaultLogger.Warn("live ws subscribe failed", "count", len(toAdd), "err", err)
		}
	}
	if len(toRemove) > 0 {
		if err := s.ws.Unsubscribe(toRemove); err != nil {
			log.DefaultLogger.Warn("live ws unsubscribe failed", "count", len(toRemove), "err", err)
		}
	}
}

func (s *LiveSubscriber) Close() {
	if s == nil || s.ws == nil {
		return
	}
	_ = s.ws.Close()
}

// quoteObservedMicros converts a Yahoo WebSocket quote time (epoch
// milliseconds) to epoch microseconds. A zero time (Yahoo omitted it)
// falls back to now.
func quoteObservedMicros(timeMs int64, now time.Time) int64 {
	if timeMs == 0 {
		return now.UnixMicro()
	}
	return timeMs * 1_000
}

func (s *LiveSubscriber) publishTick(data *yfmodels.PricingData) {
	if data == nil || data.ID == "" {
		return
	}
	up := strings.ToUpper(data.ID)
	s.mu.Lock()
	targets := s.bySymbol[up]
	s.mu.Unlock()
	if len(targets) == 0 {
		return
	}

	observedAtUs := quoteObservedMicros(data.Time, time.Now())

	majorCurrency, divisor := normalizeMinorUnits(data.Currency)
	mid := float64(data.Price) / divisor
	bid := float64(data.Bid) / divisor
	ask := float64(data.Ask) / divisor
	if bid <= 0 {
		bid = mid
	}
	if ask <= 0 {
		ask = mid
	}

	ctx := s.publishCtx.load()
	if ctx == nil {
		// No identity yet — the first tick can race the first discovery
		// pass; drop silently and rely on the next callback.
		return
	}

	for _, tgt := range targets {
		// Update liveness per (instrument, portfolio) pair.
		s.ticks.Set(tgt.InstrumentID+"|"+tgt.PortfolioID, observedAtUs)

		payloadJSON, perr := json.Marshal(map[string]any{
			"bid_price": bid,
			"ask_price": ask,
			"bid_size":  data.BidSize,
			"ask_size":  data.AskSize,
			"currency":  majorCurrency,
			"venue":     data.Exchange,
		})
		if perr != nil {
			continue
		}
		body := map[string]any{
			"source_id":    tgt.InstrumentID,
			"observed_at":  observedAtUs,
			"portfolio_id": tgt.PortfolioID,
			"payload":      string(payloadJSON),
		}
		if _, err := s.client.PublishData(ctx, QuoteNamespace, body); err != nil {
			log.DefaultLogger.Warn("live tick publish failed",
				"instrument_id", tgt.InstrumentID,
				"portfolio_id", tgt.PortfolioID,
				"err", err)
		}
	}
}
