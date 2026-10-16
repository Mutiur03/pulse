package ingest

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	collogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	logs "go.opentelemetry.io/proto/otlp/logs/v1"
	metrics "go.opentelemetry.io/proto/otlp/metrics/v1"
	resource "go.opentelemetry.io/proto/otlp/resource/v1"
	trace "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding/gzip"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type failingProducer struct{}

func (failingProducer) Produce(context.Context, string, []byte, []byte) error {
	return errors.New("kafka down")
}

func dial(t *testing.T, p Producer, a Authenticator, opts ...grpc.ServerOption) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := NewGRPCServer(p, a, nil, opts...)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.GracefulStop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func withKey(k, v string) context.Context {
	return metadata.AppendToOutgoingContext(context.Background(), k, v)
}

func span(trace8, span8, name string) *trace.Span {
	return &trace.Span{
		TraceId: []byte(strings.Repeat(trace8, 2)), SpanId: []byte(span8), Name: name,
		Kind: trace.Span_SPAN_KIND_SERVER, StartTimeUnixNano: 1_700_000_000_000_000_000, EndTimeUnixNano: 1_700_000_000_100_000_000,
	}
}

func traces(spans ...*trace.Span) *coltrace.ExportTraceServiceRequest {
	return &coltrace.ExportTraceServiceRequest{ResourceSpans: []*trace.ResourceSpans{{
		Resource:   &resource.Resource{Attributes: []*common.KeyValue{{Key: "service.name", Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "checkout"}}}}},
		ScopeSpans: []*trace.ScopeSpans{{Spans: spans}},
	}}}
}

func TestGRPCTraces(t *testing.T) {
	p := &fakeProducer{}
	c := coltrace.NewTraceServiceClient(dial(t, p, &fakeAuth{}))

	resp, err := c.Export(withKey("x-pulse-key", testKey), traces(span("abcdefgh", "12345678", "GET /cart")), grpc.UseCompressor(gzip.Name))
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetPartialSuccess() != nil {
		t.Errorf("unexpected partial success %v", resp.GetPartialSuccess())
	}
	if len(p.topics) != 1 || p.topics[0] != TopicSpans || !strings.Contains(p.vals[0], `"project_id":"42"`) || !strings.Contains(p.vals[0], `"service_name":"checkout"`) {
		t.Fatalf("produced %v %v", p.topics, p.vals)
	}

	// Bearer auth works too; a span without ids is rejected, the rest kept.
	bad := span("abcdefgh", "", "broken")
	bad.SpanId = nil
	resp, err = c.Export(withKey("authorization", "Bearer "+testKey), traces(span("abcdefgh", "87654321", "db"), bad))
	if err != nil {
		t.Fatal(err)
	}
	if ps := resp.GetPartialSuccess(); ps.GetRejectedSpans() != 1 || ps.GetErrorMessage() == "" {
		t.Errorf("partial success %v", ps)
	}
	if len(p.topics) != 2 {
		t.Errorf("want 2 spans produced, got %d", len(p.topics))
	}

	// Every span invalid: InvalidArgument, which exporters do not retry.
	_, err = c.Export(withKey("x-pulse-key", testKey), traces(bad))
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("all invalid: %v", err)
	}
}

func TestGRPCAuthAndLimits(t *testing.T) {
	auth := &fakeAuth{}
	conn := dial(t, &fakeProducer{}, auth)
	c := coltrace.NewTraceServiceClient(conn)
	req := traces(span("abcdefgh", "12345678", "x"))

	for name, ctx := range map[string]context.Context{
		"missing": context.Background(),
		"wrong":   withKey("x-pulse-key", "pk_nope"),
		"basic":   withKey("authorization", "Basic "+testKey),
	} {
		if _, err := c.Export(ctx, req); status.Code(err) != codes.Unauthenticated {
			t.Errorf("%s: %v", name, err)
		}
	}

	auth.limited = true
	_, err := c.Export(withKey("x-pulse-key", testKey), req)
	st := status.Convert(err)
	if st.Code() != codes.ResourceExhausted {
		t.Fatalf("limited: %v", err)
	}
	var retry *errdetails.RetryInfo
	for _, d := range st.Details() {
		if r, ok := d.(*errdetails.RetryInfo); ok {
			retry = r
		}
	}
	if retry == nil || retry.GetRetryDelay().AsDuration() <= 0 {
		t.Errorf("want RetryInfo, got %v", st.Details())
	}

	// Health checks need no key.
	h, err := healthpb.NewHealthClient(conn).Check(context.Background(), &healthpb.HealthCheckRequest{})
	if err != nil || h.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		t.Errorf("health %v %v", h, err)
	}
}

func TestGRPCKafkaDownIsRetryable(t *testing.T) {
	c := collogs.NewLogsServiceClient(dial(t, failingProducer{}, &fakeAuth{}))
	_, err := c.Export(withKey("x-pulse-key", testKey), &collogs.ExportLogsServiceRequest{ResourceLogs: []*logs.ResourceLogs{{
		ScopeLogs: []*logs.ScopeLogs{{LogRecords: []*logs.LogRecord{{Body: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "hi"}}}}}},
	}}})
	if status.Code(err) != codes.Unavailable {
		t.Errorf("want Unavailable, got %v", err)
	}
}

func TestGRPCMaxMessageSize(t *testing.T) {
	c := coltrace.NewTraceServiceClient(dial(t, &fakeProducer{}, &fakeAuth{}, grpc.MaxRecvMsgSize(1024)))
	big := span("abcdefgh", "12345678", strings.Repeat("x", 4096))
	if _, err := c.Export(withKey("x-pulse-key", testKey), traces(big)); status.Code(err) != codes.ResourceExhausted {
		t.Errorf("want ResourceExhausted for an oversized message, got %v", err)
	}
}

func gauge(name string, v float64) *metrics.Metric {
	return &metrics.Metric{Name: name, Unit: "1", Data: &metrics.Metric_Gauge{Gauge: &metrics.Gauge{DataPoints: []*metrics.NumberDataPoint{{
		TimeUnixNano: 1_700_000_000_000_000_000, Value: &metrics.NumberDataPoint_AsDouble{AsDouble: v},
	}}}}}
}

func TestGRPCMetrics(t *testing.T) {
	p := &fakeProducer{}
	c := colmetrics.NewMetricsServiceClient(dial(t, p, &fakeAuth{}))
	hist := &metrics.Metric{Name: "http.server.duration", Unit: "ms", Data: &metrics.Metric_Histogram{Histogram: &metrics.Histogram{
		AggregationTemporality: metrics.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA,
		DataPoints: []*metrics.HistogramDataPoint{
			{Count: 4, Sum: ptr(100.0), ExplicitBounds: []float64{10, 50}, BucketCounts: []uint64{1, 2, 1}},
			{Count: 1, Sum: ptr(1.0), ExplicitBounds: []float64{10}, BucketCounts: []uint64{1}}, // malformed
		},
	}}}
	sum := &metrics.Metric{Name: "orders", Data: &metrics.Metric_Sum{Sum: &metrics.Sum{
		IsMonotonic: true, AggregationTemporality: metrics.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE,
		DataPoints: []*metrics.NumberDataPoint{{Value: &metrics.NumberDataPoint_AsInt{AsInt: 7}}},
	}}}
	resp, err := c.Export(withKey("x-pulse-key", testKey), &colmetrics.ExportMetricsServiceRequest{ResourceMetrics: []*metrics.ResourceMetrics{{
		ScopeMetrics: []*metrics.ScopeMetrics{{Metrics: []*metrics.Metric{gauge("cpu", 0.5), hist, sum, gauge("", 1)}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := resp.GetPartialSuccess().GetRejectedDataPoints(); got != 2 {
		t.Errorf("rejected %d, want 2 (malformed histogram, nameless gauge)", got)
	}
	if len(p.vals) != 3 {
		t.Fatalf("produced %d points: %v", len(p.vals), p.vals)
	}
	for _, want := range []string{`"type":"gauge","value":0.5`, `"type":"histogram","temporality":"delta","value":25,"count":4,"sum":100`, `"type":"sum","temporality":"cumulative","monotonic":true,"value":7`} {
		if !strings.Contains(strings.Join(p.vals, "\n"), want) {
			t.Errorf("missing %s in %v", want, p.vals)
		}
	}
}

func TestHTTPMetricsJSON(t *testing.T) {
	p := &fakeProducer{}
	h := NewHandler(p, &fakeAuth{})
	body := `{"resourceMetrics":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"api"}}]},
	  "scopeMetrics":[{"metrics":[{"name":"queue.depth","gauge":{"dataPoints":[{"asInt":"12","timeUnixNano":"1700000000000000000"}]}}]}]}]}`
	rec := do(t, h, "POST", "/v1/metrics?key="+testKey, "application/json", body)
	if rec.Code != 202 || len(p.topics) != 1 || p.topics[0] != TopicMetrics || !strings.Contains(p.vals[0], `"value":12`) {
		t.Fatalf("%d %s %v", rec.Code, rec.Body, p.vals)
	}
}
func ptr[T any](v T) *T { return &v }
