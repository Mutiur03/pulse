// Package httpapi serves the project, key and allowed origin endpoints
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
	"github.com/Mutiur03/pulse/internal/keys"
	"github.com/Mutiur03/pulse/internal/modules/projects/adapters/httpapi/oapi"
	"github.com/Mutiur03/pulse/internal/modules/projects/domain"
	"github.com/Mutiur03/pulse/internal/modules/projects/service"
	"github.com/Mutiur03/pulse/internal/projects"
)

// Body limits: project names are small; origin lists can be longer.
const (
	MaxNameBody = 1 << 12
	MaxBody     = 1 << 14
)

// Handlers implements oapi.StrictServerInterface.
type Handlers struct {
	svc   *service.Service
	audit apikit.Auditor
}

var _ oapi.StrictServerInterface = (*Handlers)(nil)

// New returns the project handlers.
func New(svc *service.Service, a apikit.Auditor) *Handlers {
	return &Handlers{svc: svc, audit: a}
}

// Handler returns the routes as an oapi.ServerInterface for mounting.
func (h *Handlers) Handler() oapi.ServerInterface {
	return oapi.NewStrictHandlerWithOptions(h, []oapi.StrictMiddlewareFunc{apikit.WithRequest}, oapi.StrictHTTPServerOptions(apikit.Strict("invalid JSON body")))
}

func project2gen(p auth.Project) gen.Project {
	return gen.Project{Id: p.ID, Name: p.Name, Role: gen.ProjectRole(p.Role), OrgId: p.OrgID}
}

func apiKey(k keys.APIKey) gen.ApiKey {
	return gen.ApiKey{Id: k.ID, ProjectId: k.ProjectID, PublicKey: k.PublicKey, RateLimitPerSec: k.RateLimitPerSec,
		CreatedAt: k.CreatedAt, RevokedAt: k.RevokedAt}
}

func principal(ctx context.Context) (*auth.Principal, error) {
	p := auth.FromContext(ctx)
	if p == nil {
		return nil, apikit.Err(http.StatusUnauthorized, "not logged in")
	}
	return p, nil
}

func readable(ctx context.Context, project int64) error {
	if !apikit.CanRead(ctx, project) {
		return apikit.Err(http.StatusNotFound, "project not found")
	}
	return nil
}

func bad(err error) error { return apikit.Err(http.StatusBadRequest, err.Error()) }

// ListProjects implements GET /v1/projects.
func (h *Handlers) ListProjects(ctx context.Context, _ oapi.ListProjectsRequestObject) (oapi.ListProjectsResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	list, err := h.svc.Projects.List(ctx, p.User.ID)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Project, 0, len(list))
	for _, pr := range list {
		out = append(out, project2gen(pr))
	}
	return oapi.ListProjects200JSONResponse(out), nil
}

// CreateProject implements POST /v1/projects.
func (h *Handlers) CreateProject(ctx context.Context, req oapi.CreateProjectRequestObject) (oapi.CreateProjectResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	name, err := projects.CleanName(req.Body.Name)
	if err != nil {
		return nil, bad(err)
	}
	var org int64
	if req.Body.OrgId != nil {
		org = *req.Body.OrgId
	}
	if org != 0 {
		if _, err := apikit.OrgAccess(ctx, org, true); err != nil {
			return nil, err
		}
	} else {
		var ok bool
		if org, ok = domain.DefaultOrg(p); !ok {
			return nil, apikit.Err(http.StatusForbidden, domain.ErrNotOrgOwner.Error())
		}
	}
	pr, k, err := h.svc.Projects.Create(ctx, p.User.ID, org, name)
	if err != nil {
		return nil, err
	}
	h.audit.Record(ctx, audit.Entry{OrgID: pr.OrgID, ProjectID: pr.ID, Action: audit.ProjectCreate, Target: pr.Name})
	return oapi.CreateProject201JSONResponse(gen.CreatedProject{Project: project2gen(pr), Key: apiKey(k)}), nil
}

// UpdateProject implements PATCH /v1/projects/{project_id}.
func (h *Handlers) UpdateProject(ctx context.Context, req oapi.UpdateProjectRequestObject) (oapi.UpdateProjectResponseObject, error) {
	if err := apikit.Owner(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	name, err := projects.CleanName(req.Body.Name)
	if err != nil {
		return nil, bad(err)
	}
	name, err = h.svc.Projects.Rename(ctx, req.ProjectId, name)
	if errors.Is(err, projects.ErrNotFound) {
		return nil, apikit.Err(http.StatusNotFound, "project not found")
	} else if err != nil {
		return nil, err
	}
	h.audit.Record(ctx, audit.Entry{ProjectID: req.ProjectId, Action: audit.ProjectUpdate, Target: name})
	return oapi.UpdateProject200JSONResponse(gen.Project{Id: req.ProjectId, Name: name, Role: gen.ProjectRoleOwner}), nil
}

// ListKeys implements GET /v1/projects/{project_id}/keys.
func (h *Handlers) ListKeys(ctx context.Context, req oapi.ListKeysRequestObject) (oapi.ListKeysResponseObject, error) {
	if err := readable(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	list, err := h.svc.Keys.List(ctx, req.ProjectId)
	if err != nil {
		return nil, err
	}
	out := make([]gen.ApiKey, 0, len(list))
	for _, k := range list {
		out = append(out, apiKey(k))
	}
	return oapi.ListKeys200JSONResponse(out), nil
}

// RotateKey implements POST /v1/projects/{project_id}/keys/rotate.
func (h *Handlers) RotateKey(ctx context.Context, req oapi.RotateKeyRequestObject) (oapi.RotateKeyResponseObject, error) {
	if err := apikit.Owner(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	k, err := h.svc.Rotate(ctx, req.ProjectId, func(k keys.APIKey, revoked int) {
		h.audit.Record(ctx, audit.Entry{ProjectID: req.ProjectId, Action: audit.KeyRotate, Target: domain.KeyLabel(k.PublicKey),
			Details: map[string]any{"revoked": revoked}})
	})
	if err != nil {
		return nil, err
	}
	return oapi.RotateKey201JSONResponse(apiKey(k)), nil
}

// CreateKey implements POST /v1/projects/{project_id}/keys.
func (h *Handlers) CreateKey(ctx context.Context, req oapi.CreateKeyRequestObject) (oapi.CreateKeyResponseObject, error) {
	if err := apikit.Owner(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	k, err := h.svc.Keys.Create(ctx, req.ProjectId)
	if err != nil {
		return nil, err
	}
	h.audit.Record(ctx, audit.Entry{ProjectID: req.ProjectId, Action: audit.KeyCreate, Target: domain.KeyLabel(k.PublicKey)})
	return oapi.CreateKey201JSONResponse(apiKey(k)), nil
}

// RevokeKey implements POST /v1/projects/{project_id}/keys/{key_id}/revoke.
func (h *Handlers) RevokeKey(ctx context.Context, req oapi.RevokeKeyRequestObject) (oapi.RevokeKeyResponseObject, error) {
	if err := apikit.Owner(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	k, err := h.svc.Revoke(ctx, req.ProjectId, req.KeyId, func(k keys.APIKey) {
		h.audit.Record(ctx, audit.Entry{ProjectID: req.ProjectId, Action: audit.KeyRevoke, Target: domain.KeyLabel(k.PublicKey)})
	})
	if errors.Is(err, keys.ErrKeyNotFound) {
		return nil, apikit.Err(http.StatusNotFound, "key not found")
	} else if err != nil {
		return nil, err
	}
	return oapi.RevokeKey200JSONResponse(apiKey(k)), nil
}

// GetAllowedOrigins implements GET /v1/projects/{project_id}/allowed-origins.
func (h *Handlers) GetAllowedOrigins(ctx context.Context, req oapi.GetAllowedOriginsRequestObject) (oapi.GetAllowedOriginsResponseObject, error) {
	if err := readable(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	list, err := h.svc.AllowedOrigins(ctx, req.ProjectId)
	if err != nil {
		return nil, err
	}
	return oapi.GetAllowedOrigins200JSONResponse(gen.AllowedOrigins{Origins: list}), nil
}

// SetAllowedOrigins implements POST /v1/projects/{project_id}/allowed-origins.
func (h *Handlers) SetAllowedOrigins(ctx context.Context, req oapi.SetAllowedOriginsRequestObject) (oapi.SetAllowedOriginsResponseObject, error) {
	if err := apikit.Owner(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	clean, err := keys.CleanOrigins(req.Body.Origins)
	if err != nil {
		return nil, bad(err)
	}
	saved, err := h.svc.SetAllowedOrigins(ctx, req.ProjectId, clean)
	if errors.Is(err, domain.ErrOriginsOff) {
		return nil, apikit.Err(http.StatusServiceUnavailable, err.Error())
	} else if err != nil {
		return nil, err
	}
	h.audit.Record(ctx, audit.Entry{ProjectID: req.ProjectId, Action: audit.ProjectUpdate, Target: "allowed origins",
		Details: map[string]any{"origins": saved}})
	return oapi.SetAllowedOrigins200JSONResponse(gen.AllowedOrigins{Origins: saved}), nil
}
