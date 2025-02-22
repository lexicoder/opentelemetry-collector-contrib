// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package cloudtraceenrichment

import (
	"context"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

type metricsReporter struct {
	logger *zap.Logger
	meter  metric.Meter

	// Metrics
	apiCallsCounter      metric.Int64Counter
	spansEnrichedCounter metric.Int64Counter
	apiErrorsCounter     metric.Int64Counter
	enrichmentLatency    metric.Float64Histogram

	// Internal state for async metrics
	processor *cloudTraceProcessor
}

func newMetricsReporter(logger *zap.Logger, meter metric.Meter, processor *cloudTraceProcessor) (*metricsReporter, error) {
	r := &metricsReporter{
		logger:    logger,
		meter:     meter,
		processor: processor,
	}

	var err error

	// Create metrics
	if r.apiCallsCounter, err = meter.Int64Counter(
		"cloudtraceenrichment.api_calls",
		metric.WithDescription("The number of Cloud Trace API calls made"),
		metric.WithUnit("{calls}"),
	); err != nil {
		return nil, err
	}

	if r.spansEnrichedCounter, err = meter.Int64Counter(
		"cloudtraceenrichment.spans_enriched",
		metric.WithDescription("The number of spans that were successfully enriched"),
		metric.WithUnit("{spans}"),
	); err != nil {
		return nil, err
	}

	if r.apiErrorsCounter, err = meter.Int64Counter(
		"cloudtraceenrichment.api_errors",
		metric.WithDescription("The number of Cloud Trace API errors encountered"),
		metric.WithUnit("{errors}"),
	); err != nil {
		return nil, err
	}

	if r.enrichmentLatency, err = meter.Float64Histogram(
		"cloudtraceenrichment.enrichment_latency",
		metric.WithDescription("The latency of enrichment operations"),
		metric.WithUnit("ms"),
	); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *metricsReporter) recordMetrics(ctx context.Context, startTime time.Time, enrichedCount int) {
	// Record counters
	r.apiCallsCounter.Add(ctx, atomic.LoadInt64(&r.processor.apiCalls))
	r.spansEnrichedCounter.Add(ctx, atomic.LoadInt64(&r.processor.spansEnriched))
	r.apiErrorsCounter.Add(ctx, atomic.LoadInt64(&r.processor.apiErrors))

	// Record latency
	duration := float64(time.Since(startTime).Milliseconds())
	r.enrichmentLatency.Record(ctx, duration)
}
