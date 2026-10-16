// Package httpapi serves the dashboard endpoints through the generated
// strict interface.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/apikit"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/modules/dashboards/adapters/httpapi/oapi"
	"github.com/Mutiur03/pulse/internal/modules/dashboards/domain"
	"github.com/Mutiur03/pulse/internal/modules/dashboards/service"
)

// MaxBody is the largest dashboard body accepted.
const MaxBody = 1 << 18

// Handlers implements oapi.StrictServerInterface. A nil service means
// dashboards are not configured.
type Handlers struct{ svc *service.Service }

var _ oapi.StrictServerInterface = (*Handlers)(nil)

// New returns the dashboard handlers.
func New(svc *service.Service) *Handlers { return &Handlers{svc: svc} }

// Handler returns the routes as an oapi.ServerInterface for mounting.
func (h *Handlers) Handler() oapi.ServerInterface {
	return oapi.NewStrictHandlerWithOptions(h, nil, oapi.StrictHTTPServerOptions(apikit.Strict("invalid JSON")))
}

// actor checks access and that dashboards are configured.
func (h *Handlers) actor(ctx context.Context, project int64) (service.Actor, error) {
	p := auth.FromContext(ctx)
	if p == nil || !p.Can(project) {
		return service.Actor{}, apikit.Err(http.StatusNotFound, "project not found")
	}
	if h.svc == nil {
		return service.Actor{}, apikit.Err(http.StatusServiceUnavailable, "dashboards are not configured")
	}
	a := service.Actor{User: p.User.ID}
	for _, pr := range p.Projects {
		if pr.ID == project && pr.Role == "owner" {
			a.Owner = true
		}
	}
	return a, nil
}

func mapErr(err error) error {
	var invalid service.InvalidError
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return apikit.Err(http.StatusNotFound, "dashboard not found")
	case errors.Is(err, service.ErrForbidden):
		return apikit.Err(http.StatusForbidden, service.ErrForbidden.Error())
	case errors.As(err, &invalid):
		return apikit.Err(http.StatusBadRequest, invalid.Error())
	}
	return err
}

func toAPI(d domain.Dashboard, a service.Actor) gen.Dashboard {
	out := gen.Dashboard{
		Id: d.ID, ProjectId: d.ProjectID, Name: d.Name, Description: d.Description, Shared: d.Shared,
		CreatedBy: d.CreatedBy, CreatorName: d.CreatorName, CanEdit: domain.CanEdit(d, a.User, a.Owner),
		CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt, Widgets: make([]gen.DashboardWidget, 0, len(d.Widgets)),
	}
	for _, w := range d.Widgets {
		gw := gen.DashboardWidget{Id: w.ID, Kind: gen.DashboardWidgetKind(w.Kind)}
		title, width := w.Title, w.Width
		gw.Title, gw.Width = &title, &width
		q := map[string]interface{}{}
		_ = json.Unmarshal(w.Query, &q)
		gw.Query = &q
		out.Widgets = append(out.Widgets, gw)
	}
	return out
}

func fromAPI(in *gen.DashboardInput) (domain.Dashboard, error) {
	if in == nil {
		return domain.Dashboard{}, apikit.Err(http.StatusBadRequest, "invalid JSON")
	}
	d := domain.Dashboard{Name: in.Name}
	if in.Description != nil {
		d.Description = *in.Description
	}
	if in.Shared != nil {
		d.Shared = *in.Shared
	}
	if in.Widgets != nil {
		for _, gw := range *in.Widgets {
			wd := domain.Widget{ID: gw.Id, Kind: string(gw.Kind)}
			if gw.Title != nil {
				wd.Title = *gw.Title
			}
			if gw.Width != nil {
				wd.Width = *gw.Width
			}
			if gw.Query != nil {
				raw, err := json.Marshal(gw.Query)
				if err != nil {
					return d, apikit.Err(http.StatusBadRequest, "invalid widget query")
				}
				wd.Query = raw
			}
			d.Widgets = append(d.Widgets, wd)
		}
	}
	return d, nil
}

// ListDashboards implements GET /v1/projects/{project_id}/dashboards.
func (h *Handlers) ListDashboards(ctx context.Context, req oapi.ListDashboardsRequestObject) (oapi.ListDashboardsResponseObject, error) {
	a, err := h.actor(ctx, req.ProjectId)
	if err != nil {
		return nil, err
	}
	list, err := h.svc.List(ctx, req.ProjectId, a)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]gen.Dashboard, 0, len(list))
	for _, d := range list {
		out = append(out, toAPI(d, a))
	}
	return oapi.ListDashboards200JSONResponse(out), nil
}

// CreateDashboard implements POST /v1/projects/{project_id}/dashboards.
func (h *Handlers) CreateDashboard(ctx context.Context, req oapi.CreateDashboardRequestObject) (oapi.CreateDashboardResponseObject, error) {
	a, err := h.actor(ctx, req.ProjectId)
	if err != nil {
		return nil, err
	}
	d, err := fromAPI(req.Body)
	if err != nil {
		return nil, err
	}
	d, err = h.svc.Create(ctx, req.ProjectId, a, d)
	if err != nil {
		return nil, mapErr(err)
	}
	return oapi.CreateDashboard201JSONResponse(toAPI(d, a)), nil
}

// GetDashboard implements GET /v1/projects/{project_id}/dashboards/{dashboard_id}.
func (h *Handlers) GetDashboard(ctx context.Context, req oapi.GetDashboardRequestObject) (oapi.GetDashboardResponseObject, error) {
	a, err := h.actor(ctx, req.ProjectId)
	if err != nil {
		return nil, err
	}
	d, err := h.svc.Get(ctx, req.ProjectId, req.DashboardId, a)
	if err != nil {
		return nil, mapErr(err)
	}
	return oapi.GetDashboard200JSONResponse(toAPI(d, a)), nil
}

// UpdateDashboard implements PUT /v1/projects/{project_id}/dashboards/{dashboard_id}.
func (h *Handlers) UpdateDashboard(ctx context.Context, req oapi.UpdateDashboardRequestObject) (oapi.UpdateDashboardResponseObject, error) {
	a, err := h.actor(ctx, req.ProjectId)
	if err != nil {
		return nil, err
	}
	d, err := fromAPI(req.Body)
	if err != nil {
		return nil, err
	}
	d, err = h.svc.Update(ctx, req.ProjectId, req.DashboardId, a, d)
	if err != nil {
		return nil, mapErr(err)
	}
	return oapi.UpdateDashboard200JSONResponse(toAPI(d, a)), nil
}

// DeleteDashboard implements DELETE /v1/projects/{project_id}/dashboards/{dashboard_id}.
func (h *Handlers) DeleteDashboard(ctx context.Context, req oapi.DeleteDashboardRequestObject) (oapi.DeleteDashboardResponseObject, error) {
	a, err := h.actor(ctx, req.ProjectId)
	if err != nil {
		return nil, err
	}
	if err := h.svc.Delete(ctx, req.ProjectId, req.DashboardId, a); err != nil {
		return nil, mapErr(err)
	}
	return oapi.DeleteDashboard204Response{}, nil
}
