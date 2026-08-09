package main

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/buildinfo"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// newRouter wires the HTTP surface.
//
// This is temporary wiring. SHIP-10 introduces the eight domain packages, SHIP-12 the
// standard error contract, and SHIP-13 the /v1 route group that every product endpoint
// will live under. Routes are registered here in the meantime so the shape of the change
// stays obvious.
func newRouter(log *slog.Logger, startedAt time.Time) http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /health", healthHandler(startedAt))

	// Ordering is load-bearing: RequestID is outermost so both the log record and any
	// recovered panic can be attributed to a request, and Recover sits inside Logger so
	// a panicking handler still produces a request line with its 500.
	return httpx.Chain(mux,
		httpx.RequestID,
		httpx.Logger(log),
		httpx.Recover(log),
	)
}

// healthResponse is the body of GET /health.
//
// SHIP-19 has the Flutter app display Version from this endpoint as its proof of
// connectivity, so the field name is part of a contract with a client that will be
// installed on devices — renaming it later is a breaking change, not a tidy-up.
type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
	BuiltAt string `json:"built_at"`
	Dirty   bool   `json:"dirty"`
	Uptime  string `json:"uptime"`
}

// healthHandler reports that the process is alive and which build it is running
// (SHIP-6).
//
// It deliberately does not check PostgreSQL, Redis, or Kafka. This endpoint answers
// "should this instance be restarted or taken out of rotation", and a database blip is
// not a reason to kill every instance at once. A readiness endpoint that does check
// dependencies is a separate thing, and belongs with the deployment work rather than
// here.
//
// It is also deliberately outside the /v1 group that SHIP-13 introduces. Operational
// endpoints are consumed by load balancers and monitoring, not by API clients, and
// versioning them would mean the health check breaks on the day v2 ships.
func healthHandler(startedAt time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info := buildinfo.Get()

		httpx.WriteJSON(w, http.StatusOK, healthResponse{
			Status:  "ok",
			Version: info.Version,
			Commit:  info.Commit,
			BuiltAt: info.BuiltAt,
			Dirty:   info.Dirty,
			Uptime:  time.Since(startedAt).Round(time.Second).String(),
		})
	}
}
