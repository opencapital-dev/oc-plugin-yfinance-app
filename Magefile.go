//go:build mage
// +build mage

package main

import (
	// mage:import
	build "github.com/grafana/grafana-plugin-sdk-go/build"

	"github.com/ignacioballester/oc-plugin-sdk/pluginclient/artifacts"
)

var Default = build.BuildAll

// CopyArtifacts mirrors dashboards/ and library-panels/ into dist/ so they
// ship inside the published bundle. Run after npm run build (whose webpack
// output.clean wipes dist/ of all but the backend binaries); the SDK build
// does not copy these non-standard dirs.
func CopyArtifacts() error {
	return artifacts.Copy(".")
}

func init() {
	// confluent-kafka-go links against librdkafka via CGO. The SDK's
	// default build path sets CGO_ENABLED=0; flip it on so every target
	// compiles with cgo. The plugin-builder container ships gcc + libc
	// headers + zlib1g-dev for the static link.
	_ = build.SetBeforeBuildCallback(func(cfg build.Config) (build.Config, error) {
		cfg.EnableCGo = true
		return cfg, nil
	})
}
