// Package httpapi serves the health endpoint through the generated strict
// interface.
package httpapi

import (
	"context"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/apikit"
	"github.com/Mutiur03/pulse/internal/modules/system/adapters/httpapi/oapi"
)

// Handlers implements oapi.StrictServerInterface.
type Handlers struct{ service string }

var _ oapi.StrictServerInterface = (*Handlers)(nil)

// New returns the system handlers for the named service.
func New(service string) *Handlers { return &Handlers{service: service} }

// Handler returns the routes as an oapi.ServerInterface for mounting.
func (h *Handlers) Handler() oapi.ServerInterface {
	return oapi.NewStrictHandlerWithOptions(h, nil, oapi.StrictHTTPServerOptions(apikit.Strict("invalid JSON")))
}

// GetHealth implements GET /health.
func (h *Handlers) GetHealth(context.Context, oapi.GetHealthRequestObject) (oapi.GetHealthResponseObject, error) {
	return oapi.GetHealth200JSONResponse(gen.Health{Status: "healthy", Service: h.service}), nil
}
