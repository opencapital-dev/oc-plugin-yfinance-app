package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/grafana/grafana-plugin-sdk-go/backend/resource/httpadapter"

	"github.com/opencapital-dev/oc-plugin-sdk/pluginclient"
)

var (
	_ backend.CallResourceHandler   = (*App)(nil)
	_ instancemgmt.InstanceDisposer = (*App)(nil)
	_ backend.CheckHealthHandler    = (*App)(nil)
)

type AppOptions struct {
	DiscoveryPollSec int     `json:"pollIntervalSec"`
	YfinanceQPS      float64 `json:"qps"`
	YfinanceBurst    int     `json:"burst"`
	LiveEnable       bool    `json:"liveEnable"`
	BackfillEnable   bool    `json:"backfillEnable"`
}

type App struct {
	backend.CallResourceHandler

	client      rwPGClient
	closeClient func() error
	pluginID    string
	options     AppOptions

	yf    *YfClient
	jobs  *BackfillState
	ticks *LiveTickMap

	runtimeStarted bool
	stopBackfill   context.CancelFunc
	stopDiscovery  context.CancelFunc
	stopOptionPoll context.CancelFunc
	live           *LiveSubscriber
}

func NewApp(_ context.Context, settings backend.AppInstanceSettings) (instancemgmt.Instance, error) {
	concreteClient, err := pluginclient.NewFromSettings(pluginclient.Settings{
		JSONData:                settings.JSONData,
		DecryptedSecureJSONData: settings.DecryptedSecureJSONData,
	})
	if err != nil {
		return nil, fmt.Errorf("basic-data-app: pluginclient init: %w", err)
	}

	opts := AppOptions{
		DiscoveryPollSec: 15,
		YfinanceQPS:      1.0,
		YfinanceBurst:    3,
		LiveEnable:       true,
		BackfillEnable:   true,
	}
	if len(settings.JSONData) > 0 {
		_ = json.Unmarshal(settings.JSONData, &opts)
		if opts.DiscoveryPollSec <= 0 {
			opts.DiscoveryPollSec = 15
		}
		if opts.YfinanceQPS <= 0 {
			opts.YfinanceQPS = 1.0
		}
		if opts.YfinanceBurst <= 0 {
			opts.YfinanceBurst = 3
		}
	}

	app := &App{
		client:      concreteClient,
		closeClient: concreteClient.Close,
		pluginID:    concreteClient.Config().PluginID,
		options:     opts,
		yf:          NewYfClient(opts.YfinanceQPS, opts.YfinanceBurst),
		jobs:        NewBackfillState(),
		ticks:       NewLiveTickMap(),
	}

	mux := http.NewServeMux()
	app.registerRoutes(mux)
	app.CallResourceHandler = httpadapter.New(mux)

	go app.ensureSchema(context.Background())
	return app, nil
}

// ensureSchema creates the basic_data Postgres schema and supporting objects
// idempotently. It is called once at startup in a goroutine; errors are logged
// and surfaced so the caller can decide whether to abort.
// The first statement renames the pre-0.2.0 yfinance schema to basic_data when
// the old name still exists and the new name does not (no-op on fresh installs).
func (a *App) ensureSchema(ctx context.Context) {
	stmts := []string{
		// Idempotent rename of the pre-0.2.0 schema; no-op on fresh installs.
		`DO $$ BEGIN
		   IF EXISTS (SELECT 1 FROM information_schema.schemata WHERE schema_name = 'yfinance')
		      AND NOT EXISTS (SELECT 1 FROM information_schema.schemata WHERE schema_name = 'basic_data')
		   THEN EXECUTE 'ALTER SCHEMA yfinance RENAME TO basic_data'; END IF;
		 END $$`,
		`CREATE SCHEMA IF NOT EXISTS basic_data`,
		`CREATE TABLE IF NOT EXISTS basic_data.instrument_ticker_mapping (
    instrument_id VARCHAR NOT NULL,
    portfolio_id  VARCHAR NOT NULL,
    symbol        VARCHAR NOT NULL,
    sector        VARCHAR,
    subindustry   VARCHAR,
    vendor_meta   JSONB NOT NULL DEFAULT '{}'::jsonb,
    subscribed    BOOLEAN NOT NULL DEFAULT TRUE,
    created_at    BIGINT NOT NULL,
    updated_at    BIGINT NOT NULL,
    updated_by    VARCHAR,
    PRIMARY KEY (instrument_id, portfolio_id)
)`,
		`CREATE INDEX IF NOT EXISTS itm_symbol_idx ON basic_data.instrument_ticker_mapping(symbol)`,
		`CREATE INDEX IF NOT EXISTS itm_updated_idx ON basic_data.instrument_ticker_mapping(updated_at)`,
		`CREATE OR REPLACE VIEW basic_data.gw_classification AS
  SELECT portfolio_id AS portfolio, instrument_id, updated_at AS ts, sector, subindustry AS industry
  FROM basic_data.instrument_ticker_mapping`,
		`CREATE TABLE IF NOT EXISTS basic_data.app_settings (
		    key        text PRIMARY KEY,
		    value      text,
		    updated_at timestamptz DEFAULT now()
		)`,
	}
	for _, stmt := range stmts {
		if _, err := a.client.PGExec(ctx, stmt); err != nil {
			log.DefaultLogger.Error("basic-data-app: ensureSchema failed", "err", err, "stmt", stmt[:min(len(stmt), 60)])
			return
		}
	}
	log.DefaultLogger.Debug("basic-data-app: ensureSchema: schema ready")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (a *App) Dispose() {
	if a.stopDiscovery != nil {
		a.stopDiscovery()
	}
	if a.stopBackfill != nil {
		a.stopBackfill()
	}
	if a.stopOptionPoll != nil {
		a.stopOptionPoll()
	}
	if a.live != nil {
		a.live.Close()
	}
	if a.closeClient != nil {
		_ = a.closeClient()
	}
}

func (a *App) CheckHealth(_ context.Context, _ *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	if a.client == nil {
		return &backend.CheckHealthResult{Status: backend.HealthStatusError, Message: "pluginclient not initialised"}, nil
	}
	return &backend.CheckHealthResult{Status: backend.HealthStatusOk, Message: "ok"}, nil
}

func (a *App) handlerCtx(w http.ResponseWriter, r *http.Request) (context.Context, bool) {
	if a.client == nil {
		respondErr(w, http.StatusServiceUnavailable, "pluginclient not initialised")
		return nil, false
	}
	ctx := r.Context()
	a.ensureRuntime(ctx)
	return ctx, true
}

func (a *App) ensureRuntime(ctx context.Context) {
	if a.runtimeStarted {
		return
	}
	a.runtimeStarted = true

	runCtx := context.Background()

	if a.options.LiveEnable {
		live, err := NewLiveSubscriber(a.client, a.ticks, a.pluginID)
		if err != nil {
			log.DefaultLogger.Warn("basic-data-app: live ws init failed", "err", err)
		} else if err := live.Start(context.Background()); err != nil {
			log.DefaultLogger.Warn("basic-data-app: live ws start failed", "err", err)
		} else {
			a.live = live
		}
	}

	if a.options.BackfillEnable {
		a.stopBackfill = StartBackfillWorker(
			runCtx, a.jobs, a.yf, a.client, a, 0,
		)
	}

	a.stopDiscovery = StartDiscoveryLoop(
		runCtx, a.client, a, a.jobs, a.live, a.options.DiscoveryPollSec,
	)
	a.stopOptionPoll = StartOptionPollLoop(runCtx, a.client, a.yf, a.pluginID)
}
