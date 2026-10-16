package ingest

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	collogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	_ "google.golang.org/grpc/encoding/gzip" // accept gzip-compressed exports
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

// DefaultGRPCMaxRecv is the largest export accepted over gRPC. Collectors
// batch up to a few MiB by default.
const DefaultGRPCMaxRecv = 16 << 20

// NewGRPCServer returns an OTLP/gRPC receiver with the collector's
// TraceService, LogsService and MetricsService plus gRPC health. Clients
// authenticate with x-pulse-key metadata or "authorization: Bearer <key>",
// under the same keys and rate limits as OTLP/HTTP.
// scrub may be nil to keep records as sent.
func NewGRPCServer(p Producer, auth Authenticator, scrub ScrubSource, opts ...grpc.ServerOption) *grpc.Server {
	h := &handler{p: p, auth: auth, scrub: scrub}
	opts = append([]grpc.ServerOption{
		grpc.MaxRecvMsgSize(DefaultGRPCMaxRecv),
		grpc.ChainUnaryInterceptor(h.authorize),
	}, opts...)
	s := grpc.NewServer(opts...)
	coltrace.RegisterTraceServiceServer(s, &traceService{h: h})
	collogs.RegisterLogsServiceServer(s, &logsService{h: h})
	colmetrics.RegisterMetricsServiceServer(s, &metricsService{h: h})
	hs := health.NewServer()
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(s, hs)
	return s
}

type projectKey struct{}

func projectFrom(ctx context.Context) string {
	p, _ := ctx.Value(projectKey{}).(string)
	return p
}

// grpcKey reads the ingest key from x-pulse-key or a Bearer authorization.
func grpcKey(ctx context.Context) string {
	md, _ := metadata.FromIncomingContext(ctx)
	if v := md.Get("x-pulse-key"); len(v) > 0 && v[0] != "" {
		return v[0]
	}
	for _, v := range md.Get("authorization") {
		if k, ok := strings.CutPrefix(v, "Bearer "); ok {
			return strings.TrimSpace(k)
		}
	}
	return ""
}

// authorize checks the key and rate limit before any OTLP export. Health
// checks are open.
func (h *handler) authorize(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (interface{}, error) {
	if strings.HasPrefix(info.FullMethod, "/grpc.health.") {
		return next(ctx, req)
	}
	key := grpcKey(ctx)
	if key == "" {
		return nil, status.Error(codes.Unauthenticated, "missing ingest key: set x-pulse-key metadata")
	}
	project, err := h.auth.Authenticate(ctx, key)
	if errors.Is(err, ErrUnauthorized) {
		return nil, status.Error(codes.Unauthenticated, "invalid ingest key")
	} else if err != nil {
		log.Printf("key lookup failed: %v", err)
		return nil, status.Error(codes.Unavailable, "key lookup unavailable")
	}
	if ok, retry := h.auth.Allow(ctx, key); !ok {
		// RetryInfo tells OTLP exporters how long to back off.
		st, derr := status.New(codes.ResourceExhausted, "rate limit exceeded").
			WithDetails(&errdetails.RetryInfo{RetryDelay: durationpb.New(retry)})
		if derr != nil {
			return nil, status.Error(codes.ResourceExhausted, "rate limit exceeded")
		}
		return nil, st.Err()
	}
	return next(context.WithValue(ctx, projectKey{}, project), req)
}

// grpcErr maps a publishing error to a status code. Unavailable is
// retryable for OTLP exporters; InvalidArgument is not.
func grpcErr(e *apiError) error {
	switch e.code {
	case http.StatusBadRequest:
		return status.Error(codes.InvalidArgument, e.msg)
	case http.StatusServiceUnavailable:
		return status.Error(codes.Unavailable, e.msg)
	}
	return status.Error(codes.Internal, e.msg)
}

// rejectAll reports an export where every item was invalid.
func rejectAll(p published) error {
	if p.accepted == 0 && p.rejected > 0 {
		return status.Error(codes.InvalidArgument, p.reason)
	}
	return nil
}

type traceService struct {
	coltrace.UnimplementedTraceServiceServer
	h *handler
}

func (s *traceService) Export(ctx context.Context, req *coltrace.ExportTraceServiceRequest) (*coltrace.ExportTraceServiceResponse, error) {
	project := projectFrom(ctx)
	pub, aerr := s.h.publishTraces(ctx, project, TracesFromProto(req, project), false)
	if aerr != nil {
		return nil, grpcErr(aerr)
	}
	if err := rejectAll(pub); err != nil {
		return nil, err
	}
	resp := &coltrace.ExportTraceServiceResponse{}
	if pub.rejected > 0 {
		resp.PartialSuccess = &coltrace.ExportTracePartialSuccess{RejectedSpans: int64(pub.rejected), ErrorMessage: pub.reason}
	}
	return resp, nil
}

type logsService struct {
	collogs.UnimplementedLogsServiceServer
	h *handler
}

func (s *logsService) Export(ctx context.Context, req *collogs.ExportLogsServiceRequest) (*collogs.ExportLogsServiceResponse, error) {
	if _, aerr := s.h.publishLogs(ctx, LogsFromProto(req, projectFrom(ctx))); aerr != nil {
		return nil, grpcErr(aerr)
	}
	return &collogs.ExportLogsServiceResponse{}, nil
}

type metricsService struct {
	colmetrics.UnimplementedMetricsServiceServer
	h *handler
}

func (s *metricsService) Export(ctx context.Context, req *colmetrics.ExportMetricsServiceRequest) (*colmetrics.ExportMetricsServiceResponse, error) {
	pub, aerr := s.h.publishMetrics(ctx, MetricsFromProto(req, projectFrom(ctx)))
	if aerr != nil {
		return nil, grpcErr(aerr)
	}
	if err := rejectAll(pub); err != nil {
		return nil, err
	}
	resp := &colmetrics.ExportMetricsServiceResponse{}
	if pub.rejected > 0 {
		resp.PartialSuccess = &colmetrics.ExportMetricsPartialSuccess{RejectedDataPoints: int64(pub.rejected), ErrorMessage: pub.reason}
	}
	return resp, nil
}
