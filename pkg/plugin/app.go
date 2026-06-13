package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/grafana/grafana-plugin-sdk-go/backend/resource/httpadapter"

	"github.com/ignacioballester/oc-plugin-sdk/pluginclient"
)

var (
	_ backend.CallResourceHandler   = (*App)(nil)
	_ instancemgmt.InstanceDisposer = (*App)(nil)
	_ backend.CheckHealthHandler    = (*App)(nil)
)

// AppOptions are the yfinance-specific tunables packed into the plugin's
// AppInstanceSettings.JSONData alongside the platform fields pluginclient
// consumes. Operator-owned values; provisioning YAML sets them once per
// install.
type AppOptions struct {
	DiscoveryPollSec int     `json:"pollIntervalSec"`
	YfinanceQPS      float64 `json:"qps"`
	YfinanceBurst    int     `json:"burst"`
	LiveEnable       bool    `json:"liveEnable"`
	BackfillEnable   bool    `json:"backfillEnable"`
}

// App is the v6 yfinance-ingestor backend plugin.
//
// Writes (OHLCV bars, live quotes, tombstones) go through pluginclient to
// the gateway under (pluginID, namespace). Plugin-private state — the
// per-instrument Yahoo-symbol mapping that used to live in Postgres — moves
// to per-(plugin, org) SQLite opened via pluginclient.OpenDB.
//
// The live WS subscriber + backfill worker + discovery loop need an
// identity (for SQLite + RW access) before they can run. The full lifecycle
// kicks off only after the first authenticated CallResource request lands
// — see ensureRuntime. This avoids exposing PLATFORM_TOKEN-derived
// credentials at plugin-process boot time.
type App struct {
	backend.CallResourceHandler

	client  *pluginclient.Client
	options AppOptions

	yf    *YfClient
	jobs  *BackfillState
	ticks *LiveTickMap

	// Lazy-started lifecycle. The plugin process can serve health checks
	// before any operator request lands, so live/backfill/discovery start
	// on the first authenticated CallResource via ensureRuntime.
	runtimeStarted bool
	stopBackfill   context.CancelFunc
	stopDiscovery  context.CancelFunc
	live           *LiveSubscriber
}

func NewApp(_ context.Context, settings backend.AppInstanceSettings) (instancemgmt.Instance, error) {
	client, err := pluginclient.NewFromSettings(pluginclient.Settings{
		JSONData:                settings.JSONData,
		DecryptedSecureJSONData: settings.DecryptedSecureJSONData,
	})
	if err != nil {
		return nil, fmt.Errorf("yfinance: pluginclient init: %w", err)
	}

	opts := AppOptions{
		DiscoveryPollSec: 15,
		YfinanceQPS:      1.0,
		YfinanceBurst:    3,
		LiveEnable:       true,
		BackfillEnable:   true,
	}
	if len(settings.JSONData) > 0 {
		// Best-effort decode; missing fields stay at defaults.
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
		client:  client,
		options: opts,
		yf:      NewYfClient(opts.YfinanceQPS, opts.YfinanceBurst),
		jobs:    NewBackfillState(),
		ticks:   NewLiveTickMap(),
	}

	mux := http.NewServeMux()
	app.registerRoutes(mux)
	app.CallResourceHandler = httpadapter.New(mux)

	// Apply migrations eagerly at startup so the published gw_* contract views
	// exist for cross-plugin consumers (the core-datasource datasource reads them
	// directly and never makes a yfinance request to trigger a lazy open).
	go app.ensureMigrated()
	return app, nil
}

// ensureMigrated opens the DB once at startup to run migrations, retrying until
// an instance identity is mintable. Without this the gw_* views would only be
// created on the first authenticated request, so a freshly-installed plugin's
// published contract would be missing for consumers. Falls back to the lazy
// per-request open if the token never becomes available.
func (a *App) ensureMigrated() {
	for i := 0; i < 12; i++ {
		ctx, err := a.client.WithRequest(context.Background(), nil)
		if err == nil {
			if _, err = a.openSQLite(ctx); err == nil {
				log.DefaultLogger.Info("yfinance: startup migrations applied")
				return
			}
		}
		log.DefaultLogger.Debug("yfinance: startup migrate retry", "attempt", i, "err", err)
		time.Sleep(5 * time.Second)
	}
	log.DefaultLogger.Warn("yfinance: startup migrations not applied; gw_ views will be created lazily on first request")
}

func (a *App) Dispose() {
	if a.stopDiscovery != nil {
		a.stopDiscovery()
	}
	if a.stopBackfill != nil {
		a.stopBackfill()
	}
	if a.live != nil {
		a.live.Close()
	}
	if a.client != nil {
		_ = a.client.Close()
	}
}

func (a *App) CheckHealth(_ context.Context, _ *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	if a.client == nil {
		return &backend.CheckHealthResult{Status: backend.HealthStatusError, Message: "pluginclient not initialised"}, nil
	}
	return &backend.CheckHealthResult{Status: backend.HealthStatusOk, Message: "ok"}, nil
}

// handlerCtx wires the per-request identity into ctx via
// pluginclient.WithRequest, then triggers the lazy runtime start (live
// subscriber + backfill worker + discovery loop) the first time the
// plugin sees an authenticated request.
func (a *App) handlerCtx(w http.ResponseWriter, r *http.Request) (context.Context, bool) {
	if a.client == nil {
		respondErr(w, http.StatusServiceUnavailable, "pluginclient not initialised")
		return nil, false
	}
	ctx, err := a.client.WithRequest(r.Context(), r)
	if err != nil {
		respondErr(w, http.StatusUnauthorized, err.Error())
		return nil, false
	}
	a.ensureRuntime(ctx)
	return ctx, true
}

// ensureRuntime spins up the lazy lifecycle once. Idempotent.
func (a *App) ensureRuntime(ctx context.Context) {
	if a.runtimeStarted {
		return
	}
	a.runtimeStarted = true

	// The background loops outlive the request that lazily started them, so they
	// run on an app-lifetime context (cancelled on Dispose), never the request
	// ctx. Identity is NOT taken from the request: each loop iteration mints a
	// fresh, auto-refreshing identity via client.WithRequest — the gateway
	// publish path reads the bearer straight off the ctx identity, so a frozen
	// per-request JWT would 401 once it expired.
	runCtx := context.Background()

	if a.options.LiveEnable {
		live, err := NewLiveSubscriber(a.client, a.ticks)
		if err != nil {
			log.DefaultLogger.Warn("yfinance: live ws init failed", "err", err)
		} else if err := live.Start(context.Background()); err != nil {
			log.DefaultLogger.Warn("yfinance: live ws start failed", "err", err)
		} else {
			a.live = live
		}
	}

	if a.options.BackfillEnable {
		a.stopBackfill = StartBackfillWorker(
			runCtx, a.jobs, a.yf, a.client, 0,
		)
	}

	a.stopDiscovery = StartDiscoveryLoop(
		runCtx, a.client, a.jobs, a.live, a.options.DiscoveryPollSec,
	)
}

// errBootstrap is the sentinel ensureRuntime uses to indicate a non-fatal
// startup miss.
var errBootstrap = errors.New("yfinance: deferred runtime start")
