package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/audit"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/projects"
)

func project2gen(p auth.Project) gen.Project {
	return gen.Project{Id: p.ID, Name: p.Name, Role: gen.ProjectRole(p.Role), OrgId: p.OrgID}
}

func decodeName(w http.ResponseWriter, r *http.Request) (string, int64, bool) {
	var body gen.ProjectInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return "", 0, false
	}
	name, err := projects.CleanName(body.Name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return "", 0, false
	}
	var org int64
	if body.OrgId != nil {
		org = *body.OrgId
	}
	return name, org, true
}

func (s *Server) ListProjects(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	if p == nil {
		writeErr(w, http.StatusUnauthorized, "not logged in")
		return
	}
	list, err := s.Projects.List(r.Context(), p.User.ID)
	if err != nil {
		internal(w, err)
		return
	}
	out := make([]gen.Project, 0, len(list))
	for _, pr := range list {
		out = append(out, project2gen(pr))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) CreateProject(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	if p == nil {
		writeErr(w, http.StatusUnauthorized, "not logged in")
		return
	}
	name, org, ok := decodeName(w, r)
	if !ok {
		return
	}
	if org != 0 {
		if _, ok := orgAccess(w, r, org, true); !ok {
			return
		}
	} else if len(p.Orgs) > 0 && p.OrgRole(p.Orgs[0].ID) != "owner" {
		// Default to an organization the user owns.
		for _, o := range p.Orgs {
			if o.Role == "owner" {
				org = o.ID
				break
			}
		}
		if org == 0 {
			writeErr(w, http.StatusForbidden, "only organization owners can create projects")
			return
		}
	}
	pr, k, err := s.Projects.Create(r.Context(), p.User.ID, org, name)
	if err != nil {
		internal(w, err)
		return
	}
	s.record(r, audit.Entry{OrgID: pr.OrgID, ProjectID: pr.ID, Action: audit.ProjectCreate, Target: pr.Name})
	writeJSON(w, http.StatusCreated, gen.CreatedProject{Project: project2gen(pr), Key: apiKey(k)})
}

func (s *Server) UpdateProject(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath) {
	if !owns(w, r, projectID) {
		return
	}
	name, _, ok := decodeName(w, r)
	if !ok {
		return
	}
	name, err := s.Projects.Rename(r.Context(), projectID, name)
	if errors.Is(err, projects.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	} else if err != nil {
		internal(w, err)
		return
	}
	s.record(r, audit.Entry{ProjectID: projectID, Action: audit.ProjectUpdate, Target: name})
	writeJSON(w, http.StatusOK, gen.Project{Id: projectID, Name: name, Role: gen.ProjectRoleOwner})
}
