package ingest

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// published is what one export amounted to after validation.
type published struct {
	accepted, rejected int
	reason             string
}

// publishTraces validates spans and writes them and the errors derived from
// them to Kafka. HTTP and gRPC share it. With strict, any invalid span fails
// the whole request (the HTTP behavior); otherwise invalid spans are dropped
// and counted, for an OTLP partial success.
func (h *handler) publishTraces(ctx context.Context, project string, b TraceBatch, strict bool) (published, *apiError) {
	var pub published
	spans := b.Spans[:0]
	for i := range b.Spans {
		s := b.Spans[i]
		if s.TraceID == "" || s.SpanID == "" {
			if strict {
				return pub, &apiError{http.StatusBadRequest, "trace_id and span_id are required"}
			}
			pub.rejected++
			pub.reason = "spans need a trace_id and span_id"
			continue
		}
		s.ProjectID = project
		if s.Timestamp.IsZero() {
			s.Timestamp = time.Now().UTC()
		}
		spans = append(spans, s)
	}
	keys := make([]string, len(spans))
	vals := make([]interface{}, len(spans))
	for i := range spans {
		keys[i], vals[i] = spans[i].TraceID, &spans[i]
	}
	if aerr := h.produceAll(ctx, TopicSpans, keys, vals); aerr != nil {
		return pub, aerr
	}
	pub.accepted = len(spans)
	ekeys := make([]string, len(b.Errors))
	evals := make([]interface{}, len(b.Errors))
	for i := range b.Errors {
		ekeys[i], evals[i] = b.Errors[i].ProjectID, &b.Errors[i]
	}
	return pub, h.produceAll(ctx, TopicErrors, ekeys, evals)
}

func (h *handler) publishLogs(ctx context.Context, recs []LogRecord) (published, *apiError) {
	keys := make([]string, len(recs))
	vals := make([]interface{}, len(recs))
	for i := range recs {
		keys[i], vals[i] = recs[i].ProjectID, &recs[i]
	}
	if aerr := h.produceAll(ctx, TopicLogs, keys, vals); aerr != nil {
		return published{}, aerr
	}
	return published{accepted: len(recs)}, nil
}

func (h *handler) publishMetrics(ctx context.Context, b MetricBatch) (published, *apiError) {
	pub := published{rejected: b.Rejected, reason: b.Reason}
	keys := make([]string, len(b.Points))
	vals := make([]interface{}, len(b.Points))
	for i := range b.Points {
		p := &b.Points[i]
		// Keyed by series so one series stays ordered in its partition.
		keys[i], vals[i] = fmt.Sprintf("%s/%s/%s", p.ProjectID, p.ServiceName, p.Name), p
	}
	if aerr := h.produceAll(ctx, TopicMetrics, keys, vals); aerr != nil {
		return pub, aerr
	}
	pub.accepted = len(b.Points)
	return pub, nil
}
