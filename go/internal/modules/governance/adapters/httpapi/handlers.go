// Package httpapi serves the data governance and audit log endpoints
// through the generated strict interface.
package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/apikit"
	"github.com/Mutiur03/pulse/internal/audit"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/governance"
	"github.com/Mutiur03/pulse/internal/modules/governance/adapters/httpapi/oapi"
	"github.com/Mutiur03/pulse/internal/modules/governance/domain"
	"github.com/Mutiur03/pulse/internal/modules/governance/service"
)

// MaxBody caps request bodies.
const MaxBody = 1 << 14

// Handlers implements oapi.StrictServerInterface.
type Handlers struct {
	svc   *service.Service
	audit apikit.Auditor
}

var _ oapi.StrictServerInterface = (*Handlers)(nil)

// New returns the governance handlers.
func New(svc *service.Service, a apikit.Auditor) *Handlers {
	return &Handlers{svc: svc, audit: a}
}

// Handler returns the routes as an oapi.ServerInterface for mounting.
func (h *Handlers) Handler() oapi.ServerInterface {
	return oapi.NewStrictHandlerWithOptions(h, []oapi.StrictMiddlewareFunc{apikit.WithRequest}, oapi.StrictHTTPServerOptions(apikit.Strict("invalid JSON body")))
}

func readable(ctx context.Context, project int64) error {
	if !apikit.CanRead(ctx, project) {
		return apikit.Err(http.StatusNotFound, "project not found")
	}
	return nil
}

func (h *Handlers) ready() error {
	if !h.svc.Enabled() {
		return apikit.Err(http.StatusServiceUnavailable, domain.ErrOff.Error())
	}
	return nil
}

// read checks read access, then that governance is configured.
func (h *Handlers) read(ctx context.Context, project int64) error {
	if err := readable(ctx, project); err != nil {
		return err
	}
	return h.ready()
}

// own checks ownership, then that governance is configured.
func (h *Handlers) own(ctx context.Context, project int64) error {
	if err := apikit.Owner(ctx, project); err != nil {
		return err
	}
	return h.ready()
}

func bad(err error) error { return apikit.Err(http.StatusBadRequest, err.Error()) }

// GetDataSettings implements GET /v1/projects/{project_id}/data-settings.
func (h *Handlers) GetDataSettings(ctx context.Context, req oapi.GetDataSettingsRequestObject) (oapi.GetDataSettingsResponseObject, error) {
	if err := h.read(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	st, err := h.svc.Store.Settings(ctx, req.ProjectId)
	if err != nil {
		return nil, err
	}
	return oapi.GetDataSettings200JSONResponse(settingsOut(st)), nil
}

// UpdateDataSettings implements PUT /v1/projects/{project_id}/data-settings.
func (h *Handlers) UpdateDataSettings(ctx context.Context, req oapi.UpdateDataSettingsRequestObject) (oapi.UpdateDataSettingsResponseObject, error) {
	if err := h.own(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	in := req.Body
	st := governance.Settings{ProjectID: req.ProjectId, Rules: rulesIn(in.Scrubbing), Retention: retentionIn(in.Retention)}
	if err := st.Validate(); err != nil {
		return nil, bad(err)
	}
	saved, err := h.svc.Store.SaveSettings(ctx, st)
	if err != nil {
		return nil, err
	}
	h.audit.Record(ctx, audit.Entry{ProjectID: req.ProjectId, Action: governance.ActionSettingsUpdate, Target: "data settings",
		Details: map[string]any{"scrubbing": rulesOut(saved.Rules), "retention": retentionOut(saved.Retention)}})
	return oapi.UpdateDataSettings200JSONResponse(settingsOut(saved)), nil
}

// PreviewScrubbing implements POST /v1/projects/{project_id}/data-settings/preview.
func (h *Handlers) PreviewScrubbing(ctx context.Context, req oapi.PreviewScrubbingRequestObject) (oapi.PreviewScrubbingResponseObject, error) {
	if err := readable(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	out, err := h.svc.Scrub(rulesIn(req.Body.Scrubbing), req.Body.Sample)
	if err != nil {
		return nil, bad(err)
	}
	return oapi.PreviewScrubbing200JSONResponse(gen.ScrubPreview{Scrubbed: out}), nil
}

// ListDeletionRequests implements GET /v1/projects/{project_id}/deletion-requests.
func (h *Handlers) ListDeletionRequests(ctx context.Context, req oapi.ListDeletionRequestsRequestObject) (oapi.ListDeletionRequestsResponseObject, error) {
	if err := h.read(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	list, err := h.svc.Store.ListRequests(ctx, req.ProjectId, domain.RequestList)
	if err != nil {
		return nil, err
	}
	out := make([]gen.DeletionRequest, 0, len(list))
	for _, q := range list {
		out = append(out, requestOut(q))
	}
	return oapi.ListDeletionRequests200JSONResponse(out), nil
}

// GetDeletionRequest implements GET /v1/projects/{project_id}/deletion-requests/{request_id}.
func (h *Handlers) GetDeletionRequest(ctx context.Context, req oapi.GetDeletionRequestRequestObject) (oapi.GetDeletionRequestResponseObject, error) {
	if err := h.read(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	q, err := h.svc.Request(ctx, req.ProjectId, req.RequestId)
	if errors.Is(err, domain.ErrNoSuchDelete) {
		return nil, apikit.Err(http.StatusNotFound, err.Error())
	}
	if err != nil {
		return nil, err
	}
	return oapi.GetDeletionRequest200JSONResponse(requestOut(q)), nil
}

// CreateDeletionRequest implements POST /v1/projects/{project_id}/deletion-requests.
func (h *Handlers) CreateDeletionRequest(ctx context.Context, req oapi.CreateDeletionRequestRequestObject) (oapi.CreateDeletionRequestResponseObject, error) {
	if err := h.own(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	id, reason, err := domain.DeletionInput(req.Body.DistinctId, req.Body.Reason)
	if err != nil {
		return nil, bad(err)
	}
	var by *int64
	if p := auth.FromContext(ctx); p != nil {
		uid := p.User.ID
		by = &uid
	}
	created, err := h.svc.RequestDeletion(ctx, req.ProjectId, id, reason, by)
	if err != nil {
		return nil, err
	}
	h.audit.Record(ctx, audit.Entry{ProjectID: req.ProjectId, Action: governance.ActionDeletionRequest, Target: id,
		Details: map[string]any{"request_id": created.ID, "distinct_ids": created.DistinctIDs, "reason": reason}})
	return oapi.CreateDeletionRequest202JSONResponse(requestOut(created)), nil
}

// ListAuditLog implements GET /v1/orgs/{org_id}/audit-log.
func (h *Handlers) ListAuditLog(ctx context.Context, req oapi.ListAuditLogRequestObject) (oapi.ListAuditLogResponseObject, error) {
	if _, err := apikit.OrgAccess(ctx, req.OrgId, true); err != nil {
		return nil, err
	}
	var before int64
	if req.Params.Before != nil {
		before = *req.Params.Before
	}
	list, err := h.svc.Audit(ctx, req.OrgId, apikit.Limit(req.Params.Limit, 50, 200), before)
	if err != nil {
		return nil, err
	}
	out := make([]gen.AuditEntry, 0, len(list))
	for _, e := range list {
		g := gen.AuditEntry{Id: e.ID, CreatedAt: e.CreatedAt, Action: e.Action, ActorEmail: e.ActorEmail, Target: e.Target,
			Ip: e.IP, Details: e.Details}
		if g.Details == nil {
			g.Details = map[string]interface{}{}
		}
		if e.ActorID != 0 {
			id := e.ActorID
			g.ActorId = &id
		}
		if e.ProjectID != 0 {
			id := e.ProjectID
			g.ProjectId = &id
		}
		out = append(out, g)
	}
	return oapi.ListAuditLog200JSONResponse(out), nil
}
