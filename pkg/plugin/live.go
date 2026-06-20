package plugin

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/opencapital-dev/oc-plugin-sdk/datakey"
	yflive "github.com/wnjoon/go-yfinance/pkg/live"
	yfmodels "github.com/wnjoon/go-yfinance/pkg/models"
)

type symbolTarget struct {
	InstrumentID string
	PortfolioID  string
}

type LiveSubscriber struct {
	ws       *yflive.WebSocket
	client   rwPGClient
	ticks    *LiveTickMap
	pluginID string
	mu       sync.Mutex
	current  map[string]struct{}
	bySymbol map[string][]symbolTarget
}

func NewLiveSubscriber(client rwPGClient, ticks *LiveTickMap, pluginID string) (*LiveSubscriber, error) {
	ws, err := yflive.New()
	if err != nil {
		return nil, err
	}
	return &LiveSubscriber{
		ws:       ws,
		client:   client,
		ticks:    ticks,
		pluginID: pluginID,
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

	ctx := context.Background()

	for _, tgt := range targets {
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
		rwKey := datakey.DataKey(s.pluginID, QuoteNamespace, tgt.PortfolioID, tgt.InstrumentID, observedAtUs)
		_, err := s.client.Exec(ctx, `
			INSERT INTO data_log
				(source_namespace, source_id, portfolio_id, observed_at, ingest_ts, source, plugin_id, trace_id, payload, rw_key)
			VALUES ($1, $2, $3, to_timestamp($4::double precision / 1e6), now(), $5, $6, $7, $8, $9)
		`,
			QuoteNamespace, tgt.InstrumentID, tgt.PortfolioID, observedAtUs,
			"yahoo_ws", s.pluginID, "", string(payloadJSON), rwKey,
		)
		if err != nil {
			log.DefaultLogger.Warn("live tick publish failed",
				"instrument_id", tgt.InstrumentID,
				"portfolio_id", tgt.PortfolioID,
				"err", err)
		}
	}
}
