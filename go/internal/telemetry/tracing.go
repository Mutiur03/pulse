package telemetry

import (
	"context"
	"log"
)

type TracerProvider interface{}

func InitTracer(serviceName string) (TracerProvider, func(context.Context) error, error) {
	log.Printf("Tracer initialized for service: %s (OTel disabled in stub)", serviceName)

	cleanup := func(ctx context.Context) error {
		return nil
	}

	return nil, cleanup, nil
}
