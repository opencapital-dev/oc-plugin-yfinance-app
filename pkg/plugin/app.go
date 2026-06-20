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
	live           *LiveSubscriber
}

func NewApp(_ context.Context, settings backend.AppInstanceSettings) (instancemgmt.Instance, error) {
	concreteClient, err := pluginclient.NewFromSettings(pluginclient.Settings{
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

// ensureSchema is a no-op placeholder. The Postgres yfinance schema is created
// by the platform's init scripts, not by the plugin. Called once at startup.
func (a *App) ensureSchema(_ context.Context) {
	log.DefaultLogger.Debug("yfinance: ensureSchema: schema managed by platform init scripts")
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
			log.DefaultLogger.Warn("yfinance: live ws init failed", "err", err)
		} else if err := live.Start(context.Background()); err != nil {
			log.DefaultLogger.Warn("yfinance: live ws start failed", "err", err)
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
}
