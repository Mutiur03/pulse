package main

import (
	"github.com/Mutiur03/pulse/internal/api"
	"github.com/Mutiur03/pulse/internal/modules/dashboards"
	"github.com/Mutiur03/pulse/internal/modules/sourcemaps"
	"github.com/go-chi/chi/v5"

	"github.com/Mutiur03/pulse/internal/config"
	"github.com/Mutiur03/pulse/internal/modules/replay"
	storage "github.com/Mutiur03/pulse/internal/replay"
)

// mountModules wires every query-api module from d and mounts its routes.
// Operations of modules not moved yet are served by the legacy server.
func mountModules(r chi.Router, cfg *config.Config, d deps, srv *api.Server) {
	rd := replay.Deps{Reader: d.ch, RetentionDays: config.Int("REPLAY_RETENTION_DAYS", 0)}
	// A typed nil would look non-nil through the interface.
	if objects := storage.Connect(cfg.S3); objects != nil {
		rd.Objects = objects
	}
	replay.New(rd).Register(r)

	sd := sourcemaps.Deps{Audit: srv.Audit, ClientIP: d.proxies}
	if d.db != nil {
		sd.Repository = sourcemaps.NewStore(d.db)
	}
	if d.artifacts != nil {
		sd.Objects = d.artifacts
	}
	if d.sym != nil {
		sd.Uploaded = d.sym.Forget
	}
	sourcemaps.New(sd).Register(r)

	dashboards.New(dashboards.Deps{DB: d.db, CheckWidget: srv.CheckWidget}).Register(r)
}
