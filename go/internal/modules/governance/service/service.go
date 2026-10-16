// Package service runs the governance use cases.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Mutiur03/pulse/internal/audit"
	"github.com/Mutiur03/pulse/internal/governance"
	"github.com/Mutiur03/pulse/internal/modules/governance/domain"
	"github.com/Mutiur03/pulse/internal/modules/governance/ports"
)

// Service manages data settings, deletion requests and the audit log. A nil
// Store disables settings and deletions; nil Persons deletes only the given
// id; a nil AuditLog lists nothing.
type Service struct {
	Store    ports.Store
	Persons  ports.Persons
	AuditLog ports.AuditLog
}

// Enabled reports whether settings and deletions are served.
func (s *Service) Enabled() bool { return s.Store != nil }

// Scrub previews rules on a text or JSON sample.
func (s *Service) Scrub(rules governance.Rules, sample string) (string, error) {
	if len(sample) > domain.MaxSample {
		return "", domain.ErrSampleLong
	}
	rules, err := rules.Clean()
	if err != nil {
		return "", err
	}
	sc, err := governance.Compile(rules)
	if err != nil {
		return "", err
	}
	if trimmed := strings.TrimSpace(sample); strings.HasPrefix(trimmed, "{") && json.Valid([]byte(trimmed)) {
		return string(sc.JSON([]byte(trimmed))), nil
	}
	return sc.String(sample), nil
}

// Request returns one deletion request.
func (s *Service) Request(ctx context.Context, project, id int64) (governance.Request, error) {
	q, err := s.Store.GetRequest(ctx, project, id)
	if errors.Is(err, governance.ErrNotFound) {
		return q, domain.ErrNoSuchDelete
	}
	return q, err
}

// RequestDeletion queues deletion of every id linked to distinctID.
func (s *Service) RequestDeletion(ctx context.Context, project int64, distinctID, reason string, by *int64) (governance.Request, error) {
	req := governance.Request{ProjectID: project, DistinctID: distinctID, PersonKey: distinctID, DistinctIDs: []string{distinctID}, Reason: reason, RequestedBy: by}
	if s.Persons != nil {
		key, err := s.Persons.ResolvePerson(ctx, uint64(project), distinctID)
		if err != nil {
			return req, err
		}
		ids, err := s.Persons.LinkedIDs(ctx, uint64(project), []string{distinctID, key})
		if err != nil {
			return req, err
		}
		req.PersonKey, req.DistinctIDs = key, ids
	}
	return s.Store.CreateRequest(ctx, req)
}

// Audit lists an organization's audit log, newest first.
func (s *Service) Audit(ctx context.Context, org int64, limit int, before int64) ([]audit.Entry, error) {
	if s.AuditLog == nil {
		return nil, nil
	}
	return s.AuditLog.List(ctx, org, limit, before)
}
