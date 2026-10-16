// Package service runs the analytics use cases.
package service

import (
	"context"
	"time"

	"github.com/Mutiur03/pulse/internal/modules/analytics/domain"
	"github.com/Mutiur03/pulse/internal/modules/analytics/ports"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

// Service answers analytics queries. Nil Trends or People disable those
// queries.
type Service struct {
	events ports.Events
	trends ports.Trends
	people ports.People
}

// New returns a Service.
func New(events ports.Events, trends ports.Trends, people ports.People) *Service {
	return &Service{events: events, trends: trends, people: people}
}

// Events lists product events.
func (s *Service) Events(ctx context.Context, f ch.EventFilter) ([]ch.ExploredEvent, error) {
	return s.events.ListEvents(ctx, f)
}

// EventNames lists event names with counts.
func (s *Service) EventNames(ctx context.Context, project uint64, from, to time.Time) ([]ch.EventNameCount, error) {
	return s.events.EventNames(ctx, project, from, to)
}

// Funnel runs a validated funnel query.
func (s *Service) Funnel(ctx context.Context, q ch.FunnelQuery) (ch.FunnelData, error) {
	return s.events.RunFunnel(ctx, q)
}

// Trend runs a validated trend query.
func (s *Service) Trend(ctx context.Context, q ch.TrendQuery) ([]ch.TrendSeriesData, error) {
	if s.trends == nil {
		return nil, domain.ErrInsightsOff
	}
	return s.trends.RunTrend(ctx, q)
}

// PeopleEnabled reports whether persons, retention and paths are served.
func (s *Service) PeopleEnabled() bool { return s.people != nil }

// Persons lists persons.
func (s *Service) Persons(ctx context.Context, f ch.PersonFilter) ([]ch.PersonRow, error) {
	if s.people == nil {
		return nil, domain.ErrAnalyticsOff
	}
	return s.people.ListPersons(ctx, f)
}

// PersonDetail is a person with recent sessions and events.
type PersonDetail struct {
	Person   ch.PersonRow
	Sessions []ch.PersonSession
	Events   []ch.EventRecord
}

// Person resolves id (a person or distinct id) and loads its detail.
func (s *Service) Person(ctx context.Context, project uint64, id string, from, to time.Time) (PersonDetail, error) {
	var d PersonDetail
	if s.people == nil {
		return d, domain.ErrAnalyticsOff
	}
	key, err := s.people.ResolvePerson(ctx, project, id)
	if err != nil {
		return d, err
	}
	row, found, err := s.people.GetPerson(ctx, project, key, from, to)
	if err != nil {
		return d, err
	}
	if !found {
		return d, domain.ErrPersonNotFound
	}
	d.Person = row
	if d.Sessions, err = s.people.PersonSessions(ctx, project, key, from, to, domain.PersonSessions); err != nil {
		return d, err
	}
	d.Events, err = s.people.PersonEvents(ctx, project, key, from, to, domain.PersonEvents)
	return d, err
}

// Retention runs a validated retention query.
func (s *Service) Retention(ctx context.Context, q ch.RetentionQuery) (ch.RetentionData, error) {
	if s.people == nil {
		return ch.RetentionData{}, domain.ErrAnalyticsOff
	}
	return s.people.RunRetention(ctx, q)
}

// Paths runs a validated paths query.
func (s *Service) Paths(ctx context.Context, q ch.PathsQuery) ([]ch.PathRow, error) {
	if s.people == nil {
		return nil, domain.ErrAnalyticsOff
	}
	return s.people.RunPaths(ctx, q)
}
