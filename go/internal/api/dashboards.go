package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/dashboards"
)

// DashboardStore keeps custom dashboards.
type DashboardStore interface {
	List(ctx context.Context, project, user int64) ([]dashboards.Dashboard, error)
	Get(ctx context.Context, project, id, user int64) (dashboards.Dashboard, error)
	Create(ctx context.Context, d dashboards.Dashboard) (dashboards.Dashboard, error)
	Update(ctx context.Context, d dashboards.Dashboard, user int64) (dashboards.Dashboard, error)
	Delete(ctx context.Context, project, id int64) error
}

// dashboardsReady checks access and that dashboards are configured, and
// returns the user and whether they own the project.
func (s *Server) dashboardsReady(w http.ResponseWriter, r *http.Request, project int64) (int64, bool, bool) {
	p := auth.FromContext(r.Context())
	if p == nil || !p.Can(project) {
		writeErr(w, http.StatusNotFound, "project not found")
		return 0, false, false
	}
	if s.Dashboards == nil {
		writeErr(w, http.StatusServiceUnavailable, "dashboards are not configured")
		return 0, false, false
	}
	owner := false
	for _, pr := range p.Projects {
		if pr.ID == project && pr.Role == "owner" {
			owner = true
		}
	}
	return p.User.ID, owner, true
}

func dashboardOut(d dashboards.Dashboard, user int64, owner bool) gen.Dashboard {
	out := gen.Dashboard{
		Id: d.ID, ProjectId: d.ProjectID, Name: d.Name, Description: d.Description, Shared: d.Shared,
		CreatedBy: d.CreatedBy, CreatorName: d.CreatorName, CanEdit: dashboards.CanEdit(d, user, owner),
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

// dashboardIn decodes and validates a dashboard body.
func (s *Server) dashboardIn(w http.ResponseWriter, r *http.Request) (dashboards.Dashboard, bool) {
	var in gen.DashboardInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<18)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return dashboards.Dashboard{}, false
	}
	d := dashboards.Dashboard{Name: in.Name}
	if in.Description != nil {
		d.Description = *in.Description
	}
	if in.Shared != nil {
		d.Shared = *in.Shared
	}
	if in.Widgets != nil {
		for _, gw := range *in.Widgets {
			wd := dashboards.Widget{ID: gw.Id, Kind: string(gw.Kind)}
			if gw.Title != nil {
				wd.Title = *gw.Title
			}
			if gw.Width != nil {
				wd.Width = *gw.Width
			}
			if gw.Query != nil {
				raw, err := json.Marshal(gw.Query)
				if err != nil {
					writeErr(w, http.StatusBadRequest, "invalid widget query")
					return d, false
				}
				wd.Query = raw
			}
			d.Widgets = append(d.Widgets, wd)
		}
	}
	if err := dashboards.Clean(&d, s.widgetCheck); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return d, false
	}
	return d, true
}

func (s *Server) ListDashboards(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath) {
	user, owner, ok := s.dashboardsReady(w, r, projectID)
	if !ok {
		return
	}
	list, err := s.Dashboards.List(r.Context(), projectID, user)
	if err != nil {
		internal(w, err)
		return
	}
	out := make([]gen.Dashboard, 0, len(list))
	for _, d := range list {
		out = append(out, dashboardOut(d, user, owner))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) CreateDashboard(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath) {
	user, owner, ok := s.dashboardsReady(w, r, projectID)
	if !ok {
		return
	}
	d, ok := s.dashboardIn(w, r)
	if !ok {
		return
	}
	d.ProjectID, d.CreatedBy = projectID, user
	d, err := s.Dashboards.Create(r.Context(), d)
	if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, dashboardOut(d, user, owner))
}

func (s *Server) GetDashboard(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath, id gen.DashboardPath) {
	user, owner, ok := s.dashboardsReady(w, r, projectID)
	if !ok {
		return
	}
	d, err := s.Dashboards.Get(r.Context(), projectID, id, user)
	if errors.Is(err, dashboards.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "dashboard not found")
		return
	} else if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dashboardOut(d, user, owner))
}

// editable loads a dashboard and checks the user may change it.
func (s *Server) editable(w http.ResponseWriter, r *http.Request, project, id, user int64, owner bool) bool {
	cur, err := s.Dashboards.Get(r.Context(), project, id, user)
	if errors.Is(err, dashboards.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "dashboard not found")
		return false
	} else if err != nil {
		internal(w, err)
		return false
	}
	if !dashboards.CanEdit(cur, user, owner) {
		writeErr(w, http.StatusForbidden, "only the creator or a project owner can change this dashboard")
		return false
	}
	return true
}

func (s *Server) UpdateDashboard(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath, id gen.DashboardPath) {
	user, owner, ok := s.dashboardsReady(w, r, projectID)
	if !ok || !s.editable(w, r, projectID, id, user, owner) {
		return
	}
	d, ok := s.dashboardIn(w, r)
	if !ok {
		return
	}
	d.ID, d.ProjectID = id, projectID
	d, err := s.Dashboards.Update(r.Context(), d, user)
	if errors.Is(err, dashboards.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "dashboard not found")
		return
	} else if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dashboardOut(d, user, owner))
}

func (s *Server) DeleteDashboard(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath, id gen.DashboardPath) {
	user, owner, ok := s.dashboardsReady(w, r, projectID)
	if !ok || !s.editable(w, r, projectID, id, user, owner) {
		return
	}
	if err := s.Dashboards.Delete(r.Context(), projectID, id); err != nil && !errors.Is(err, dashboards.ErrNotFound) {
		internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
