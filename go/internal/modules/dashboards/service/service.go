// Package service implements the dashboard use cases.
package service

import (
	"context"
	"errors"

	"github.com/Mutiur03/pulse/internal/modules/dashboards/domain"
	"github.com/Mutiur03/pulse/internal/modules/dashboards/ports"
)

// ErrForbidden means the user may see but not change the dashboard.
var ErrForbidden = errors.New("only the creator or a project owner can change this dashboard")

// InvalidError is a dashboard that fails validation.
type InvalidError struct{ Err error }

func (e InvalidError) Error() string { return e.Err.Error() }

// Actor is the user acting on a project's dashboards.
type Actor struct {
	User  int64
	Owner bool // project owner
}

// Service manages dashboards.
type Service struct {
	repo  ports.Repository
	check domain.QueryCheck
}

// New returns a Service; check validates widget queries.
func New(repo ports.Repository, check domain.QueryCheck) *Service {
	return &Service{repo: repo, check: check}
}

// List returns the dashboards the actor can see.
func (s *Service) List(ctx context.Context, project int64, a Actor) ([]domain.Dashboard, error) {
	return s.repo.List(ctx, project, a.User)
}

// Get returns one dashboard the actor can see.
func (s *Service) Get(ctx context.Context, project, id int64, a Actor) (domain.Dashboard, error) {
	return s.repo.Get(ctx, project, id, a.User)
}

// Create validates and stores a new dashboard owned by the actor.
func (s *Service) Create(ctx context.Context, project int64, a Actor, d domain.Dashboard) (domain.Dashboard, error) {
	if err := domain.Clean(&d, s.check); err != nil {
		return d, InvalidError{err}
	}
	d.ProjectID, d.CreatedBy = project, a.User
	return s.repo.Create(ctx, d)
}

// editable loads a dashboard and checks the actor may change it.
func (s *Service) editable(ctx context.Context, project, id int64, a Actor) error {
	cur, err := s.repo.Get(ctx, project, id, a.User)
	if err != nil {
		return err
	}
	if !domain.CanEdit(cur, a.User, a.Owner) {
		return ErrForbidden
	}
	return nil
}

// Update replaces a dashboard the actor may change.
func (s *Service) Update(ctx context.Context, project, id int64, a Actor, d domain.Dashboard) (domain.Dashboard, error) {
	if err := s.editable(ctx, project, id, a); err != nil {
		return d, err
	}
	if err := domain.Clean(&d, s.check); err != nil {
		return d, InvalidError{err}
	}
	d.ID, d.ProjectID = id, project
	return s.repo.Update(ctx, d, a.User)
}

// Delete removes a dashboard the actor may change; deleting one that is
// already gone succeeds.
func (s *Service) Delete(ctx context.Context, project, id int64, a Actor) error {
	if err := s.editable(ctx, project, id, a); err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, project, id); err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	return nil
}
