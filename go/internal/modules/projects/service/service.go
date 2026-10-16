// Package service runs the project and key use cases.
package service

import (
	"context"

	"github.com/Mutiur03/pulse/internal/keys"
	"github.com/Mutiur03/pulse/internal/modules/projects/domain"
	"github.com/Mutiur03/pulse/internal/modules/projects/ports"
)

// Service manages projects, ingest keys and allowed origins. Nil Origins
// lists none and refuses changes.
type Service struct {
	Projects ports.Projects
	Keys     ports.Keys
	Origins  ports.Origins
}

// Rotate replaces the project's keys and purges the old ones from the
// gateway cache.
func (s *Service) Rotate(ctx context.Context, project int64, audit func(k keys.APIKey, revoked int)) (keys.APIKey, error) {
	k, revoked, err := s.Keys.Rotate(ctx, project)
	if err != nil {
		return k, err
	}
	audit(k, len(revoked))
	for _, old := range revoked {
		// On failure the cache entry still expires within keys.PositiveTTL.
		if err := s.Keys.Purge(ctx, old); err != nil {
			return k, err
		}
	}
	return k, nil
}

// Revoke revokes a key and purges it from the gateway cache.
func (s *Service) Revoke(ctx context.Context, project, id int64, audit func(k keys.APIKey)) (keys.APIKey, error) {
	k, err := s.Keys.Revoke(ctx, project, id)
	if err != nil {
		return k, err
	}
	audit(k)
	return k, s.Keys.Purge(ctx, k.PublicKey)
}

// AllowedOrigins lists the project's allowed origins.
func (s *Service) AllowedOrigins(ctx context.Context, project int64) ([]string, error) {
	if s.Origins == nil {
		return []string{}, nil
	}
	return s.Origins.AllowedOrigins(ctx, project)
}

// SetAllowedOrigins replaces the allowed origins with cleaned ones.
func (s *Service) SetAllowedOrigins(ctx context.Context, project int64, clean []string) ([]string, error) {
	if s.Origins == nil {
		return nil, domain.ErrOriginsOff
	}
	return s.Origins.SetAllowedOrigins(ctx, project, clean)
}
