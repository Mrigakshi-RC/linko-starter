package main

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace" // 1. Ensure this is imported
)

// 1. Declare the package-level tracer
var tracer trace.Tracer

func initTracing(ctx context.Context) (func(context.Context) error, error) {
	exp, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(2*time.Second)),
		sdktrace.WithResource(resource.Default()),
	)

	otel.SetTracerProvider(tp)

	// 1. Initialize the tracer with the correct scope name
	tracer = tp.Tracer("boot.dev/linko")

	return tp.Shutdown, nil
}
