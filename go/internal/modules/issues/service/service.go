// Package service runs the issue use cases.
package service

import (
	"context"
	"time"

	"github.com/Mutiur03/pulse/internal/modules/issues/domain"
	"github.com/Mutiur03/pulse/internal/modules/issues/ports"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

// Service lists and triages issues.
type Service struct {
	repo ports.Repository
	occ  ports.Occurrences
}

// New returns a Service.
func New(repo ports.Repository, occ ports.Occurrences) *Service {
	return &Service{repo: repo, occ: occ}
}

// Page is one page of issues with their hourly trends.
type Page struct {
	Issues []domain.Issue
	Total  int64
	Trends map[uint64][]int64
}

// List returns a page of issues with their trends.
func (s *Service) List(ctx context.Context, f domain.ListFilter, now time.Time) (Page, error) {
	list, total, err := s.repo.List(ctx, f)
	if err != nil {
		return Page{}, err
	}
	ids := make([]uint64, len(list))
	for i, iss := range list {
		ids[i] = uint64(iss.ID)
	}
	trends, err := s.occ.IssueTrends(ctx, uint64(f.ProjectID), ids, now)
	if err != nil {
		return Page{}, err
	}
	return Page{Issues: list, Total: total, Trends: trends}, nil
}

// Get returns an issue the caller can read; canRead hides other projects'
// issues as not found.
func (s *Service) Get(ctx context.Context, id int64, canRead func(project int64) bool) (domain.Issue, error) {
	iss, err := s.repo.Get(ctx, id)
	if err == nil && !canRead(iss.ProjectID) {
		err = domain.ErrNotFound
	}
	return iss, err
}

// Recent returns an issue's latest occurrences.
func (s *Service) Recent(ctx context.Context, iss domain.Issue, limit int) ([]ch.ErrorRecord, error) {
	return s.occ.ErrorsByIssue(ctx, uint64(iss.ProjectID), uint64(iss.ID), limit)
}

// SetStatus changes the status of an issue the caller can read.
func (s *Service) SetStatus(ctx context.Context, id int64, st domain.Status, canRead func(project int64) bool) (domain.Issue, error) {
	if _, err := s.Get(ctx, id, canRead); err != nil {
		return domain.Issue{}, err
	}
	return s.repo.SetStatus(ctx, id, st)
}
