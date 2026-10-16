package main

import (
	"github.com/go-chi/chi/v5"

	"github.com/Mutiur03/pulse/internal/config"
	"github.com/Mutiur03/pulse/internal/modules/replay"
	storage "github.com/Mutiur03/pulse/internal/replay"
)

// mountModules wires every query-api module from d and mounts its routes.
// Operations of modules not moved yet are served by the legacy server.
func mountModules(r chi.Router, cfg *config.Config, d deps) {
	rd := replay.Deps{Reader: d.ch, RetentionDays: config.Int("REPLAY_RETENTION_DAYS", 0)}
	// A typed nil would look non-nil through the interface.
	if objects := storage.Connect(cfg.S3); objects != nil {
		rd.Objects = objects
	}
	replay.New(rd).Register(r)
}
