package ingest

import (
	"math"
	"testing"

	"github.com/Mutiur03/pulse/internal/golden"
	colmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	metrics "go.opentelemetry.io/proto/otlp/metrics/v1"
	resource "go.opentelemetry.io/proto/otlp/resource/v1"
)

const ts = uint64(1760500000000000000)

func TestMapTracesGolden(t *testing.T) {
	goRes := rawResource{
		attrs: kv{"service.name": "api", "telemetry.sdk.language": "go", "service.version": "1.2.3",
			"deployment.environment": "prod", "pulse.project_id": "999"},
		spans: []rawSpan{
			{traceID: "t1", spanID: "s1", name: "GET /pay", kind: 2, start: ts, end: ts + 5e6,
				attrs:      kv{"http.status_code": "500", "session.id": "sess", "enduser.id": "u1", "http.route": "/pay"},
				statusCode: 2, statusMsg: "bad"},
			{traceID: "t1", spanID: "s2", parentID: "s1", name: "charge", kind: 3, start: ts + 1e6, end: ts + 2e6,
				events: []rawEvent{
					{name: "exception", time: ts + 15e5, attrs: kv{"exception.type": "CardError", "exception.message": "declined",
						"exception.stacktrace": "at charge"}},
					{name: "exception", time: ts + 16e5, attrs: kv{}},
					{name: "log", time: ts + 17e5, attrs: kv{"k": "v"}},
				}},
			{traceID: "t2", spanID: "s3", name: "SELECT", kind: 3, start: ts + 9e6, end: ts + 1e6, statusCode: 2,
				attrs: kv{"db.system": "postgresql", "db.statement": "SELECT 1", "error.type": "DeadlockError"}},
			{traceID: "t3", spanID: "s4", name: "worker", kind: 99, start: ts, end: ts + 1, statusCode: 2,
				attrs: kv{"pulse.session_id": "s-alt", "pulse.distinct_id": "d-alt", "url.full": "https://x/y?z=1"}},
			{traceID: "t3", spanID: "s5", parentID: "s4", name: "child", start: ts, end: ts + 2, statusCode: 1},
		},
	}
	webRes := rawResource{
		attrs: kv{"service.name": "web", "telemetry.sdk.language": "webjs"},
		spans: []rawSpan{{traceID: "t1", spanID: "w1", name: "click", start: ts, end: ts + 1e6, statusCode: 2,
			events: []rawEvent{{name: "exception", time: ts, attrs: kv{"exception.message": "boom"}}}}},
	}
	golden.JSON(t, "map_traces", mapTraces([]rawResource{goRes, webRes}, "42"))
	golden.JSON(t, "map_traces_empty", mapTraces(nil, "42"))
}

func gaugePoint(v float64, t uint64) *metrics.NumberDataPoint {
	return &metrics.NumberDataPoint{TimeUnixNano: t, Value: &metrics.NumberDataPoint_AsDouble{AsDouble: v},
		Attributes: []*common.KeyValue{{Key: "host", Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "h1"}}}}}
}

func TestMetricsFromProtoGolden(t *testing.T) {
	m := func(name string, data any) *metrics.Metric {
		x := &metrics.Metric{Name: name, Description: "d", Unit: "1"}
		switch d := data.(type) {
		case *metrics.Metric_Gauge:
			x.Data = d
		case *metrics.Metric_Sum:
			x.Data = d
		case *metrics.Metric_Histogram:
			x.Data = d
		case *metrics.Metric_ExponentialHistogram:
			x.Data = d
		case *metrics.Metric_Summary:
			x.Data = d
		}
		return x
	}
	long := make([]byte, 256)
	for i := range long {
		long[i] = 'a'
	}
	req := &colmetrics.ExportMetricsServiceRequest{ResourceMetrics: []*metrics.ResourceMetrics{{
		Resource: &resource.Resource{Attributes: []*common.KeyValue{{Key: "service.name",
			Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "api"}}}}},
		ScopeMetrics: []*metrics.ScopeMetrics{{Metrics: []*metrics.Metric{
			m("cpu", &metrics.Metric_Gauge{Gauge: &metrics.Gauge{DataPoints: []*metrics.NumberDataPoint{gaugePoint(0.5, ts), gaugePoint(math.NaN(), ts)}}}),
			m("reqs", &metrics.Metric_Sum{Sum: &metrics.Sum{IsMonotonic: true,
				AggregationTemporality: metrics.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE,
				DataPoints:             []*metrics.NumberDataPoint{{TimeUnixNano: ts, Value: &metrics.NumberDataPoint_AsInt{AsInt: 12}}}}}),
			m("latency", &metrics.Metric_Histogram{Histogram: &metrics.Histogram{
				AggregationTemporality: metrics.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA,
				DataPoints: []*metrics.HistogramDataPoint{
					{TimeUnixNano: ts, Count: 4, Sum: proto64(10), Min: proto64(1), Max: proto64(4), ExplicitBounds: []float64{1, 2}, BucketCounts: []uint64{1, 2, 1}},
					{TimeUnixNano: ts, Count: 1, BucketCounts: []uint64{1}, ExplicitBounds: []float64{1, 2}},
					{TimeUnixNano: ts},
				}}}),
			m("exp", &metrics.Metric_ExponentialHistogram{ExponentialHistogram: &metrics.ExponentialHistogram{
				DataPoints: []*metrics.ExponentialHistogramDataPoint{{TimeUnixNano: ts, Count: 2, Sum: proto64(3)}, {TimeUnixNano: ts}}}}),
			m("sum", &metrics.Metric_Summary{Summary: &metrics.Summary{
				DataPoints: []*metrics.SummaryDataPoint{{TimeUnixNano: ts, Count: 3, Sum: 9}, {TimeUnixNano: ts, Sum: math.Inf(1)}}}}),
			m("", &metrics.Metric_Gauge{Gauge: &metrics.Gauge{DataPoints: []*metrics.NumberDataPoint{gaugePoint(1, ts)}}}),
			m(string(long), &metrics.Metric_Sum{Sum: &metrics.Sum{DataPoints: []*metrics.NumberDataPoint{gaugePoint(1, ts), gaugePoint(2, ts)}}}),
			{Name: "empty"},
		}}},
	}}}
	golden.JSON(t, "metrics_from_proto", MetricsFromProto(req, "42"))
}

func proto64(v float64) *float64 { return &v }
