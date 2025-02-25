// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package googlecloudtraceprocessor

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	trace "cloud.google.com/go/trace/apiv1"
	tracepb "cloud.google.com/go/trace/apiv1/tracepb"
	"github.com/golang/groupcache/lru"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/otel/metric/noop"
	"go.uber.org/zap"
	"google.golang.org/api/option"
)

const (
	traceparentHeader       = "traceparent"
	cloudTraceContextHeader = "x-cloud-trace-context"
	enrichedAttribute       = "cloudtrace.enriched"
	projectAttribute        = "cloudtrace.project_id"
)

// traceClient is an interface that wraps the Cloud Trace client methods we use
type traceClient interface {
	GetTrace(ctx context.Context, projectID, traceID string) (*tracepb.Trace, error)
	Close() error
}

// cloudTraceClientWrapper wraps the Cloud Trace client to implement our interface
type cloudTraceClientWrapper struct {
	client *trace.Client
}

func (w *cloudTraceClientWrapper) GetTrace(ctx context.Context, projectID, traceID string) (*tracepb.Trace, error) {
	req := &tracepb.GetTraceRequest{
		ProjectId: projectID,
		TraceId:   traceID,
	}
	return w.client.GetTrace(ctx, req)
}

func (w *cloudTraceClientWrapper) Close() error {
	return w.client.Close()
}

type cloudTraceProcessor struct {
	logger       *zap.Logger
	config       *Config
	nextConsumer consumer.Traces
	client       traceClient
	cache        *lru.Cache
	lock         sync.RWMutex
	metrics      *metricsReporter

	// Metrics
	apiCalls      int64
	spansEnriched int64
	apiErrors     int64
}

func newProcessor(logger *zap.Logger, config *Config, nextConsumer consumer.Traces) (processor.Traces, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	var opts []option.ClientOption
	if config.CredentialsFile != "" {
		opts = append(opts, option.WithCredentialsFile(config.CredentialsFile))
	}

	client, err := trace.NewClient(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create Cloud Trace client: %w", err)
	}

	p := &cloudTraceProcessor{
		logger:       logger,
		config:       config,
		nextConsumer: nextConsumer,
		client:       &cloudTraceClientWrapper{client: client},
		cache:        lru.New(config.CacheSize),
	}

	return p, nil
}

func (p *cloudTraceProcessor) Start(_ context.Context, _ component.Host) error {
	// Initialize metrics reporter
	meter := noop.NewMeterProvider().Meter("googlecloudtrace")
	reporter, err := newMetricsReporter(p.logger, meter, p)
	if err != nil {
		return fmt.Errorf("failed to create metrics reporter: %w", err)
	}
	p.metrics = reporter

	return nil
}

func (p *cloudTraceProcessor) Shutdown(context.Context) error {
	return p.client.Close()
}

func (p *cloudTraceProcessor) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: true}
}

func (p *cloudTraceProcessor) ConsumeTraces(ctx context.Context, td ptrace.Traces) error {
	startTime := time.Now()
	enrichedCount := 0

	rss := td.ResourceSpans()
	for i := 0; i < rss.Len(); i++ {
		rs := rss.At(i)
		ilss := rs.ScopeSpans()
		for j := 0; j < ilss.Len(); j++ {
			ils := ilss.At(j)
			spans := ils.Spans()
			for k := 0; k < spans.Len(); k++ {
				span := spans.At(k)
				if err := p.enrichSpan(ctx, span); err != nil {
					p.logger.Error("Failed to enrich span", zap.Error(err))
					atomic.AddInt64(&p.apiErrors, 1)
				} else {
					enrichedCount++
				}
			}
		}
	}

	atomic.AddInt64(&p.spansEnriched, int64(enrichedCount))

	// Record metrics
	if p.metrics != nil {
		p.metrics.recordMetrics(ctx, startTime, enrichedCount)
	}

	// Record enrichment latency
	duration := time.Since(startTime).Milliseconds()
	p.logger.Debug("Trace enrichment completed",
		zap.Int("enriched_spans", enrichedCount),
		zap.Int64("duration_ms", duration))

	return p.nextConsumer.ConsumeTraces(ctx, td)
}

func (p *cloudTraceProcessor) enrichSpan(ctx context.Context, span ptrace.Span) error {
	// Skip if already enriched
	if _, exists := span.Attributes().Get(enrichedAttribute); exists {
		return nil
	}

	traceID := p.extractTraceID(span)
	if traceID == "" {
		return fmt.Errorf("no valid trace ID found in span")
	}

	// Check cache first
	p.lock.RLock()
	if data, ok := p.cache.Get(traceID); ok {
		p.lock.RUnlock()
		enrichData := data.(*tracepb.Trace)
		return p.applyEnrichment(span, enrichData)
	}
	p.lock.RUnlock()

	// Fetch from Cloud Trace API
	atomic.AddInt64(&p.apiCalls, 1)
	trace, err := p.client.GetTrace(ctx, p.config.ProjectID, traceID)
	if err != nil {
		atomic.AddInt64(&p.apiErrors, 1)
		return fmt.Errorf("failed to get trace from Cloud Trace: %w", err)
	}

	// Update cache
	p.lock.Lock()
	p.cache.Add(traceID, trace)
	p.lock.Unlock()

	return p.applyEnrichment(span, trace)
}

func (p *cloudTraceProcessor) applyEnrichment(span ptrace.Span, trace *tracepb.Trace) error {
	attrs := span.Attributes()

	// Mark as enriched
	attrs.PutBool(enrichedAttribute, true)
	attrs.PutStr(projectAttribute, p.config.ProjectID)

	// Add Cloud Trace specific attributes
	if trace.ProjectId != "" {
		attrs.PutStr("cloudtrace.project_id", trace.ProjectId)
	}

	// Find matching span in Cloud Trace data
	spanIDBytes := span.SpanID()
	spanIDInt := uint64(0)
	for i := 0; i < 8; i++ {
		spanIDInt = (spanIDInt << 8) | uint64(spanIDBytes[i])
	}

	for _, cloudSpan := range trace.Spans {
		if cloudSpan.SpanId == spanIDInt {
			// Enrich with Cloud Trace specific data
			if cloudSpan.Labels != nil {
				for key, value := range cloudSpan.Labels {
					attrs.PutStr("cloudtrace.label."+key, value)
				}
			}
			break
		}
	}

	return nil
}

func (p *cloudTraceProcessor) extractTraceID(span ptrace.Span) string {
	attrs := span.Attributes()

	// Try W3C traceparent first if configured
	if p.config.PreferTraceparent {
		if val, exists := attrs.Get(traceparentHeader); exists {
			parts := strings.Split(val.Str(), "-")
			if len(parts) >= 2 {
				return parts[1]
			}
		}
	}

	// Try Cloud Trace Context
	if val, exists := attrs.Get(cloudTraceContextHeader); exists {
		parts := strings.Split(val.Str(), "/")
		if len(parts) >= 1 {
			return parts[0]
		}
	}

	// Fallback to W3C traceparent if not preferred but available
	if !p.config.PreferTraceparent {
		if val, exists := attrs.Get(traceparentHeader); exists {
			parts := strings.Split(val.Str(), "-")
			if len(parts) >= 2 {
				return parts[1]
			}
		}
	}

	// Use span's trace ID as last resort
	return span.TraceID().String()
}
