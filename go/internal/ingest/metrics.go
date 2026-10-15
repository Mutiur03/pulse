package ingest

import (
	"math"
	"time"

	colmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	metrics "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// TopicMetrics carries one MetricPoint per data point.
const TopicMetrics = "raw.metrics"

// MetricPoint is one OTLP data point, flattened.
type MetricPoint struct {
	ProjectID   string    `json:"project_id"`
	Timestamp   time.Time `json:"timestamp"`
	ServiceName string    `json:"service_name,omitempty"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Unit        string    `json:"unit,omitempty"`
	// Type is gauge, sum, histogram, exponential_histogram or summary.
	Type string `json:"type"`
	// Temporality is delta or cumulative for sums and histograms.
	Temporality string  `json:"temporality,omitempty"`
	Monotonic   bool    `json:"monotonic,omitempty"`
	Value       float64 `json:"value"`
	// Count, Sum, Min, Max, Bounds and Buckets describe distributions.
	Count      uint64            `json:"count,omitempty"`
	Sum        float64           `json:"sum,omitempty"`
	Min        float64           `json:"min,omitempty"`
	Max        float64           `json:"max,omitempty"`
	Bounds     []float64         `json:"bounds,omitempty"`
	Buckets    []uint64          `json:"buckets,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// MetricBatch is a decoded metrics export; Rejected counts data points that
// were dropped, with the reason of the last one.
type MetricBatch struct {
	Points   []MetricPoint
	Rejected int
	Reason   string
}

// DecodeOTLPMetrics decodes an OTLP/HTTP metrics export, protobuf or JSON.
func DecodeOTLPMetrics(body []byte, isProto bool, project string) (MetricBatch, error) {
	var req colmetrics.ExportMetricsServiceRequest
	var err error
	if isProto {
		err = proto.Unmarshal(body, &req)
	} else {
		err = protojson.UnmarshalOptions{DiscardUnknown: true}.Unmarshal(body, &req)
	}
	if err != nil {
		return MetricBatch{}, err
	}
	return MetricsFromProto(&req, project), nil
}

var temporalities = map[metrics.AggregationTemporality]string{
	metrics.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA:      "delta",
	metrics.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE: "cumulative",
}

func pointTime(t uint64) time.Time {
	if t == 0 {
		return time.Now().UTC()
	}
	return nanos(t)
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// MetricsFromProto flattens an export into data points.
func MetricsFromProto(req *colmetrics.ExportMetricsServiceRequest, project string) MetricBatch {
	var b MetricBatch
	reject := func(n int, why string) {
		b.Rejected += n
		b.Reason = why
	}
	for _, rm := range req.GetResourceMetrics() {
		res := pbAttrs(rm.GetResource().GetAttributes())
		service := first(res, "service.name")
		for _, sm := range rm.GetScopeMetrics() {
			for _, m := range sm.GetMetrics() {
				base := MetricPoint{
					ProjectID: project, ServiceName: service, Name: m.GetName(),
					Description: clip(m.GetDescription()), Unit: m.GetUnit(),
				}
				add := func(p MetricPoint, ts uint64, attrs kv) {
					p.Timestamp = pointTime(ts)
					p.Attributes = make(map[string]string, len(attrs))
					for k, v := range attrs {
						p.Attributes[k] = clip(v)
					}
					if !finite(p.Value) || !finite(p.Sum) {
						reject(1, "non-finite value in "+p.Name)
						return
					}
					b.Points = append(b.Points, p)
				}
				number := func(dp *metrics.NumberDataPoint) float64 {
					if v, ok := dp.GetValue().(*metrics.NumberDataPoint_AsInt); ok {
						return float64(v.AsInt)
					}
					return dp.GetAsDouble()
				}
				if m.GetName() == "" || len(m.GetName()) > 255 {
					reject(dataPoints(m), "metric name must be 1 to 255 characters")
					continue
				}
				switch d := m.GetData().(type) {
				case *metrics.Metric_Gauge:
					for _, dp := range d.Gauge.GetDataPoints() {
						p := base
						p.Type, p.Value = "gauge", number(dp)
						add(p, dp.GetTimeUnixNano(), pbAttrs(dp.GetAttributes()))
					}
				case *metrics.Metric_Sum:
					for _, dp := range d.Sum.GetDataPoints() {
						p := base
						p.Type, p.Value = "sum", number(dp)
						p.Temporality, p.Monotonic = temporalities[d.Sum.GetAggregationTemporality()], d.Sum.GetIsMonotonic()
						add(p, dp.GetTimeUnixNano(), pbAttrs(dp.GetAttributes()))
					}
				case *metrics.Metric_Histogram:
					for _, dp := range d.Histogram.GetDataPoints() {
						if len(dp.GetBucketCounts()) > 0 && len(dp.GetBucketCounts()) != len(dp.GetExplicitBounds())+1 {
							reject(1, "histogram bucket counts must be one more than bounds in "+m.GetName())
							continue
						}
						p := base
						p.Type = "histogram"
						p.Temporality = temporalities[d.Histogram.GetAggregationTemporality()]
						p.Count, p.Sum, p.Min, p.Max = dp.GetCount(), dp.GetSum(), dp.GetMin(), dp.GetMax()
						p.Bounds, p.Buckets = dp.GetExplicitBounds(), dp.GetBucketCounts()
						if p.Count > 0 {
							p.Value = p.Sum / float64(p.Count)
						}
						add(p, dp.GetTimeUnixNano(), pbAttrs(dp.GetAttributes()))
					}
				case *metrics.Metric_ExponentialHistogram:
					// Kept as count, sum and mean; buckets are not stored.
					for _, dp := range d.ExponentialHistogram.GetDataPoints() {
						p := base
						p.Type = "exponential_histogram"
						p.Temporality = temporalities[d.ExponentialHistogram.GetAggregationTemporality()]
						p.Count, p.Sum, p.Min, p.Max = dp.GetCount(), dp.GetSum(), dp.GetMin(), dp.GetMax()
						if p.Count > 0 {
							p.Value = p.Sum / float64(p.Count)
						}
						add(p, dp.GetTimeUnixNano(), pbAttrs(dp.GetAttributes()))
					}
				case *metrics.Metric_Summary:
					for _, dp := range d.Summary.GetDataPoints() {
						p := base
						p.Type, p.Count, p.Sum = "summary", dp.GetCount(), dp.GetSum()
						if p.Count > 0 {
							p.Value = p.Sum / float64(p.Count)
						}
						add(p, dp.GetTimeUnixNano(), pbAttrs(dp.GetAttributes()))
					}
				default:
					reject(0, "metric "+m.GetName()+" has no data")
				}
			}
		}
	}
	return b
}

func dataPoints(m *metrics.Metric) int {
	switch d := m.GetData().(type) {
	case *metrics.Metric_Gauge:
		return len(d.Gauge.GetDataPoints())
	case *metrics.Metric_Sum:
		return len(d.Sum.GetDataPoints())
	case *metrics.Metric_Histogram:
		return len(d.Histogram.GetDataPoints())
	case *metrics.Metric_ExponentialHistogram:
		return len(d.ExponentialHistogram.GetDataPoints())
	case *metrics.Metric_Summary:
		return len(d.Summary.GetDataPoints())
	}
	return 0
}
