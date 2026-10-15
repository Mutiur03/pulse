package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/projects"
)

func project2gen(p auth.Project) gen.Project {
	return gen.Project{Id: p.ID, Name: p.Name, Role: gen.ProjectRole(p.Role)}
}

func decodeName(w http.ResponseWriter, r *http.Request) (string, bool) {
	var body gen.ProjectInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return "", false
	}
	name, err := projects.CleanName(body.Name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return "", false
	}
	return name, true
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
	name, ok := decodeName(w, r)
	if !ok {
		return
	}
	pr, k, err := s.Projects.Create(r.Context(), p.User.ID, name)
	if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, gen.CreatedProject{Project: project2gen(pr), Key: apiKey(k)})
}

func (s *Server) UpdateProject(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath) {
	if !owns(w, r, projectID) {
		return
	}
	name, ok := decodeName(w, r)
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
	writeJSON(w, http.StatusOK, gen.Project{Id: projectID, Name: name, Role: gen.Owner})
}
