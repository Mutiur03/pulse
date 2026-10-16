package apikit

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	strictnethttp "github.com/oapi-codegen/runtime/strictmiddleware/nethttp"

	"github.com/Mutiur03/pulse/internal/audit"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/clientip"
)

type requestKey struct{}

// WithRequest is a strict middleware that keeps the HTTP request in the
// context, for handlers that read headers or the client address.
func WithRequest(f strictnethttp.StrictHTTPHandlerFunc, _ string) strictnethttp.StrictHTTPHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req interface{}) (interface{}, error) {
		return f(context.WithValue(ctx, requestKey{}, r), w, r, req)
	}
}

// Request returns the request stored by WithRequest, or nil.
func Request(ctx context.Context) *http.Request {
	r, _ := ctx.Value(requestKey{}).(*http.Request)
	return r
}

// CanRead reports whether the logged-in user can read the project.
func CanRead(ctx context.Context, project int64) bool {
	p := auth.FromContext(ctx)
	return p != nil && p.Can(project)
}

// Owner checks the logged-in user owns the project: 404 when they cannot
// read it, 403 when they are not an owner.
func Owner(ctx context.Context, project int64) error {
	p := auth.FromContext(ctx)
	if p == nil || !p.Can(project) {
		return Err(http.StatusNotFound, "project not found")
	}
	for _, pr := range p.Projects {
		if pr.ID == project && pr.Role != "owner" {
			return Err(http.StatusForbidden, "only project owners can do this")
		}
	}
	return nil
}

// Auditor writes audit entries for module handlers. A nil Recorder drops
// them.
type Auditor struct {
	Recorder audit.Recorder
	ClientIP *clientip.Resolver
}

// Record writes e, filling in the actor, address and org. Failures are
// logged and do not fail the request.
func (a Auditor) Record(ctx context.Context, e audit.Entry) {
	if a.Recorder == nil {
		return
	}
	p := auth.FromContext(ctx)
	if p != nil && e.ActorID == 0 {
		e.ActorID, e.ActorEmail = p.User.ID, p.User.Email
	}
	if r := Request(ctx); e.IP == "" && r != nil {
		e.IP = a.ClientIP.IP(r)
	}
	if e.OrgID == 0 && e.ProjectID != 0 && p != nil {
		for _, pr := range p.Projects {
			if pr.ID == e.ProjectID {
				e.OrgID = pr.OrgID
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := a.Recorder.Record(ctx, e); err != nil {
		slog.Error("audit log write failed", "action", e.Action, "err", err)
	}
}
