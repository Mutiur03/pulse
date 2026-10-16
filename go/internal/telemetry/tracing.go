package telemetry

import (
	"context"
	"log/slog"
)

type TracerProvider interface{}

func InitTracer(serviceName string) (TracerProvider, func(context.Context) error, error) {
	slog.Info("tracer initialized (OTel disabled in stub)", "service", serviceName)

	cleanup := func(ctx context.Context) error {
		return nil
	}

	return nil, cleanup, nil
}
