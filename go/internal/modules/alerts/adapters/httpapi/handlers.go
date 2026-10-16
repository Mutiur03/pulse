// Package httpapi serves the alert rule and channel endpoints through the
// generated strict interface.
package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/Mutiur03/pulse/internal/alerts"
	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/apikit"
	"github.com/Mutiur03/pulse/internal/audit"
	"github.com/Mutiur03/pulse/internal/modules/alerts/adapters/httpapi/oapi"
	"github.com/Mutiur03/pulse/internal/modules/alerts/domain"
	"github.com/Mutiur03/pulse/internal/modules/alerts/service"
)

// MaxBody caps request bodies.
const MaxBody = 1 << 14

// Handlers implements oapi.StrictServerInterface.
type Handlers struct {
	svc   *service.Service
	audit apikit.Auditor
	clock apikit.Clock
}

var _ oapi.StrictServerInterface = (*Handlers)(nil)

// New returns the alert handlers.
func New(svc *service.Service, a apikit.Auditor, clock apikit.Clock) *Handlers {
	return &Handlers{svc: svc, audit: a, clock: clock}
}

// Handler returns the routes as an oapi.ServerInterface for mounting.
func (h *Handlers) Handler() oapi.ServerInterface {
	return oapi.NewStrictHandlerWithOptions(h, []oapi.StrictMiddlewareFunc{apikit.WithRequest}, oapi.StrictHTTPServerOptions(apikit.Strict("invalid JSON body")))
}

// ready checks access and that alerting is configured.
func (h *Handlers) ready(ctx context.Context, project int64) error {
	if !apikit.CanRead(ctx, project) {
		return apikit.Err(http.StatusNotFound, "project not found")
	}
	if !h.svc.Enabled() {
		return apikit.Err(http.StatusServiceUnavailable, domain.ErrAlertsOff.Error())
	}
	return nil
}

// owner also requires a project owner: channels decide where project data
// is sent, so only owners manage them; members can list them and edit rules.
func (h *Handlers) owner(ctx context.Context, project int64) error {
	if err := h.ready(ctx, project); err != nil {
		return err
	}
	return apikit.Owner(ctx, project)
}

func storeErr(err error) error {
	if errors.Is(err, alerts.ErrNotFound) {
		return apikit.Err(http.StatusNotFound, "not found")
	}
	return err
}

func bad(err error) error { return apikit.Err(http.StatusBadRequest, err.Error()) }

// ListAlerts implements GET /v1/projects/{project_id}/alerts.
func (h *Handlers) ListAlerts(ctx context.Context, req oapi.ListAlertsRequestObject) (oapi.ListAlertsResponseObject, error) {
	if err := h.ready(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	rules, err := h.svc.Store.ListRules(ctx, req.ProjectId)
	if err != nil {
		return nil, err
	}
	out := make([]gen.AlertRule, 0, len(rules))
	for _, x := range rules {
		out = append(out, alertRule(x))
	}
	return oapi.ListAlerts200JSONResponse(out), nil
}

// CreateAlert implements POST /v1/projects/{project_id}/alerts.
func (h *Handlers) CreateAlert(ctx context.Context, req oapi.CreateAlertRequestObject) (oapi.CreateAlertResponseObject, error) {
	if err := h.ready(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	rule, err := ruleFromInput(req.ProjectId, *req.Body)
	if err != nil {
		return nil, bad(err)
	}
	created, err := h.svc.Store.CreateRule(ctx, rule)
	if err != nil {
		return nil, err
	}
	return oapi.CreateAlert201JSONResponse(alertRule(created)), nil
}

// GetAlert implements GET /v1/projects/{project_id}/alerts/{alert_id}.
func (h *Handlers) GetAlert(ctx context.Context, req oapi.GetAlertRequestObject) (oapi.GetAlertResponseObject, error) {
	if err := h.ready(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	rule, err := h.svc.Store.GetRule(ctx, req.ProjectId, req.AlertId)
	if err != nil {
		return nil, storeErr(err)
	}
	hist, err := h.svc.Store.History(ctx, rule.ID, domain.HistoryLimit)
	if err != nil {
		return nil, err
	}
	out := gen.AlertDetail{Rule: alertRule(rule), History: make([]gen.AlertHistoryEntry, 0, len(hist))}
	for _, e := range hist {
		out.History = append(out.History, gen.AlertHistoryEntry{
			Id: e.ID, At: e.At, State: gen.AlertState(e.State), Previous: gen.AlertState(e.Previous), Value: e.Value,
			Notice: string(e.Notice), Suppressed: e.Suppressed, Failures: e.Failures, IssueId: e.IssueID, TraceId: e.TraceID, Message: e.Message,
		})
	}
	return oapi.GetAlert200JSONResponse(out), nil
}

// UpdateAlert implements PUT /v1/projects/{project_id}/alerts/{alert_id}.
func (h *Handlers) UpdateAlert(ctx context.Context, req oapi.UpdateAlertRequestObject) (oapi.UpdateAlertResponseObject, error) {
	if err := h.ready(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	rule, err := ruleFromInput(req.ProjectId, *req.Body)
	if err != nil {
		return nil, bad(err)
	}
	rule.ID = req.AlertId
	updated, err := h.svc.Store.UpdateRule(ctx, rule)
	if err != nil {
		return nil, storeErr(err)
	}
	return oapi.UpdateAlert200JSONResponse(alertRule(updated)), nil
}

// DeleteAlert implements DELETE /v1/projects/{project_id}/alerts/{alert_id}.
func (h *Handlers) DeleteAlert(ctx context.Context, req oapi.DeleteAlertRequestObject) (oapi.DeleteAlertResponseObject, error) {
	if err := h.ready(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	if err := h.svc.Store.DeleteRule(ctx, req.ProjectId, req.AlertId); err != nil {
		return nil, storeErr(err)
	}
	return oapi.DeleteAlert204Response{}, nil
}

// MuteAlert implements POST /v1/projects/{project_id}/alerts/{alert_id}/mute.
func (h *Handlers) MuteAlert(ctx context.Context, req oapi.MuteAlertRequestObject) (oapi.MuteAlertResponseObject, error) {
	if err := h.ready(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	until, err := domain.MuteUntil(h.clock.Now(), req.Body.Minutes)
	if err != nil {
		return nil, bad(err)
	}
	rule, err := h.svc.Store.Mute(ctx, req.ProjectId, req.AlertId, until)
	if err != nil {
		return nil, storeErr(err)
	}
	return oapi.MuteAlert200JSONResponse(alertRule(rule)), nil
}

// PreviewAlert implements POST /v1/projects/{project_id}/alerts/preview.
func (h *Handlers) PreviewAlert(ctx context.Context, req oapi.PreviewAlertRequestObject) (oapi.PreviewAlertResponseObject, error) {
	if !apikit.CanRead(ctx, req.ProjectId) {
		return nil, apikit.Err(http.StatusNotFound, "project not found")
	}
	rule, err := ruleFromInput(req.ProjectId, *req.Body)
	if err != nil {
		return nil, bad(err)
	}
	p, err := h.svc.Preview(ctx, rule, h.clock.Now().UTC())
	if err != nil {
		return nil, err
	}
	out := gen.AlertPreview{Points: make([]gen.AlertPoint, 0, len(p.Points)), Threshold: rule.Threshold, Comparison: gen.AlertComparison(rule.Comparison),
		Unit: rule.Metric.Unit(), BucketSeconds: int(p.Bucket.Seconds()), Supported: p.Supported, Current: p.Current}
	for _, pt := range p.Points {
		out.Points = append(out.Points, gen.AlertPoint{At: pt.At, Value: pt.Value})
	}
	return oapi.PreviewAlert200JSONResponse(out), nil
}

// ListAlertChannels implements GET /v1/projects/{project_id}/alert-channels.
func (h *Handlers) ListAlertChannels(ctx context.Context, req oapi.ListAlertChannelsRequestObject) (oapi.ListAlertChannelsResponseObject, error) {
	if err := h.ready(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	list, err := h.svc.Store.ListChannels(ctx, req.ProjectId)
	if err != nil {
		return nil, err
	}
	out := make([]gen.AlertChannel, 0, len(list))
	for _, c := range list {
		out = append(out, alertChannel(c, false))
	}
	return oapi.ListAlertChannels200JSONResponse(out), nil
}

// CreateAlertChannel implements POST /v1/projects/{project_id}/alert-channels.
func (h *Handlers) CreateAlertChannel(ctx context.Context, req oapi.CreateAlertChannelRequestObject) (oapi.CreateAlertChannelResponseObject, error) {
	if err := h.owner(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	in := req.Body
	c := alerts.Channel{ProjectID: req.ProjectId, Name: in.Name, Kind: alerts.ChannelKind(in.Kind), Target: in.Target}
	if err := c.Validate(); err != nil {
		return nil, bad(err)
	}
	if err := h.svc.CheckTarget(ctx, c); err != nil {
		return nil, apikit.Err(http.StatusBadRequest, "target is not allowed: "+err.Error())
	}
	created, err := h.svc.Store.CreateChannel(ctx, c)
	if err != nil {
		return nil, err
	}
	h.audit.Record(ctx, audit.Entry{ProjectID: req.ProjectId, Action: audit.AlertChannelCreate, Target: c.Name, Details: map[string]any{"kind": string(c.Kind)}})
	return oapi.CreateAlertChannel201JSONResponse(alertChannel(created, true)), nil
}

// DeleteAlertChannel implements DELETE /v1/projects/{project_id}/alert-channels/{channel_id}.
func (h *Handlers) DeleteAlertChannel(ctx context.Context, req oapi.DeleteAlertChannelRequestObject) (oapi.DeleteAlertChannelResponseObject, error) {
	if err := h.owner(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	if err := h.svc.Store.DeleteChannel(ctx, req.ProjectId, req.ChannelId); err != nil {
		return nil, storeErr(err)
	}
	h.audit.Record(ctx, audit.Entry{ProjectID: req.ProjectId, Action: audit.AlertChannelDelete, Target: "channel " + strconv.FormatInt(req.ChannelId, 10)})
	return oapi.DeleteAlertChannel204Response{}, nil
}

// TestAlertChannel implements POST /v1/projects/{project_id}/alert-channels/{channel_id}/test.
func (h *Handlers) TestAlertChannel(ctx context.Context, req oapi.TestAlertChannelRequestObject) (oapi.TestAlertChannelResponseObject, error) {
	if err := h.owner(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	c, err := h.svc.Store.GetChannel(ctx, req.ProjectId, req.ChannelId)
	if err != nil {
		return nil, storeErr(err)
	}
	if !h.svc.SenderEnabled() {
		return nil, apikit.Err(http.StatusServiceUnavailable, domain.ErrSenderOff.Error())
	}
	out := gen.AlertChannelTest{Ok: true}
	if err := h.svc.TestChannel(ctx, c, h.clock.Now()); err != nil {
		msg := err.Error()
		out = gen.AlertChannelTest{Ok: false, Error: &msg}
	}
	return oapi.TestAlertChannel200JSONResponse(out), nil
}
