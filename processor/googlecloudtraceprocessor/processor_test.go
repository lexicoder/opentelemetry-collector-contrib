// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package googlecloudtraceprocessor

import (
	"context"
	"encoding/hex"
	"testing"
	"time"

	tracepb "cloud.google.com/go/trace/apiv1/tracepb"
	"github.com/golang/groupcache/lru"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
)

type mockCloudTraceClient struct {
	mockResponse *tracepb.Trace
}

func (m *mockCloudTraceClient) GetTrace(_ context.Context, _, _ string) (*tracepb.Trace, error) {
	if m.mockResponse != nil {
		return m.mockResponse, nil
	}
	return nil, nil
}

func (m *mockCloudTraceClient) Close() error {
	return nil
}

func loadTestTraces(t *testing.T) ptrace.Traces {
	traces := ptrace.NewTraces()
	resourceSpans := traces.ResourceSpans().AppendEmpty()

	// Add resource attributes
	resourceSpans.Resource().Attributes().PutStr("service.name", "test-service")

	scopeSpans := resourceSpans.ScopeSpans().AppendEmpty()
	scopeSpans.Scope().SetName("test-scope")

	// Add first span with traceparent
	span1 := scopeSpans.Spans().AppendEmpty()
	traceID, err := hex.DecodeString("0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	span1.SetTraceID(pcommon.TraceID(traceID))
	spanID, err := hex.DecodeString("0123456789abcdef")
	require.NoError(t, err)
	span1.SetSpanID(pcommon.SpanID(spanID))
	span1.SetName("test-span")
	span1.SetKind(ptrace.SpanKindClient)
	span1.Attributes().PutStr(traceparentHeader, "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01")

	// Add second span with x-cloud-trace-context
	span2 := scopeSpans.Spans().AppendEmpty()
	traceID2, err := hex.DecodeString("fedcba9876543210fedcba9876543210")
	require.NoError(t, err)
	span2.SetTraceID(pcommon.TraceID(traceID2))
	spanID2, err := hex.DecodeString("fedcba9876543210")
	require.NoError(t, err)
	span2.SetSpanID(pcommon.SpanID(spanID2))
	span2.SetName("test-span-2")
	span2.SetKind(ptrace.SpanKindClient)
	span2.Attributes().PutStr(cloudTraceContextHeader, "fedcba9876543210fedcba9876543210/fedcba9876543210")

	return traces
}

func loadCloudTraceResponse() *tracepb.Trace {
	// Create a test trace with known data
	trace := &tracepb.Trace{
		ProjectId: "test-project",
		TraceId:   "0123456789abcdef0123456789abcdef",
		Spans: []*tracepb.TraceSpan{
			{
				SpanId: 0x0123456789abcdef,
				Name:   "test-span",
				Labels: map[string]string{
					"cloud.region":     "us-central1",
					"service.version":  "1.0.0",
					"http.method":      "GET",
					"http.url":         "https://example.com/api",
					"http.status_code": "200",
				},
			},
			{
				SpanId: 0xfedcba9876543210,
				Name:   "test-span-2",
				Labels: map[string]string{
					"cloud.region":     "us-central1",
					"service.version":  "1.0.0",
					"http.method":      "POST",
					"http.url":         "https://example.com/api/v2",
					"http.status_code": "201",
				},
			},
		},
	}
	return trace
}

func TestNewProcessor(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		shouldError bool
	}{
		{
			name: "Valid config",
			config: &Config{
				ProjectID:         "test-project",
				PreferTraceparent: true,
				CacheSize:         1000,
				CacheTTL:          5 * time.Minute,
			},
			shouldError: false,
		},
		{
			name: "Invalid credentials file",
			config: &Config{
				ProjectID:         "test-project",
				CredentialsFile:   "/nonexistent/file.json",
				PreferTraceparent: true,
				CacheSize:         1000,
				CacheTTL:          5 * time.Minute,
			},
			shouldError: true,
		},
		{
			name: "Invalid cache size",
			config: &Config{
				ProjectID:         "test-project",
				PreferTraceparent: true,
				CacheSize:         -1,
				CacheTTL:          5 * time.Minute,
			},
			shouldError: true,
		},
		{
			name: "Invalid cache TTL",
			config: &Config{
				ProjectID:         "test-project",
				PreferTraceparent: true,
				CacheSize:         1000,
				CacheTTL:          0,
			},
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zap.NewNop()
			nextConsumer := consumertest.NewNop()

			processor, err := newProcessor(logger, tt.config, nextConsumer)
			if tt.shouldError {
				assert.Error(t, err)
				assert.Nil(t, processor)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, processor)
			}
		})
	}
}

func TestProcessorLifecycle(t *testing.T) {
	config := &Config{
		ProjectID:         "test-project",
		PreferTraceparent: true,
		CacheSize:         1000,
		CacheTTL:          5 * time.Minute,
	}

	logger := zap.NewNop()
	nextConsumer := consumertest.NewNop()

	processor, err := newProcessor(logger, config, nextConsumer)
	require.NoError(t, err)
	require.NotNil(t, processor)

	// Test Start
	err = processor.Start(context.Background(), componenttest.NewNopHost())
	assert.NoError(t, err)

	// Test Shutdown
	err = processor.Shutdown(context.Background())
	assert.NoError(t, err)
}

func TestProcessorCapabilities(t *testing.T) {
	config := &Config{
		ProjectID:         "test-project",
		PreferTraceparent: true,
		CacheSize:         1000,
		CacheTTL:          5 * time.Minute,
	}

	logger := zap.NewNop()
	nextConsumer := consumertest.NewNop()

	processor, err := newProcessor(logger, config, nextConsumer)
	require.NoError(t, err)
	require.NotNil(t, processor)

	capabilities := processor.Capabilities()
	assert.True(t, capabilities.MutatesData)
}

func TestProcessorConsumeTraces(t *testing.T) {
	tests := []struct {
		name         string
		config       *Config
		mockResponse *tracepb.Trace
		validateFunc func(t *testing.T, traces ptrace.Traces)
	}{
		{
			name: "Process traces with traceparent",
			config: &Config{
				ProjectID:         "test-project",
				PreferTraceparent: true,
				CacheSize:         1000,
				CacheTTL:          5 * time.Minute,
			},
			mockResponse: loadCloudTraceResponse(),
			validateFunc: func(t *testing.T, traces ptrace.Traces) {
				// Validate that spans were enriched with Cloud Trace data
				rspans := traces.ResourceSpans()
				require.Equal(t, 1, rspans.Len())

				spans := rspans.At(0).ScopeSpans().At(0).Spans()
				require.Equal(t, 2, spans.Len())

				// Check first span (traceparent)
				span := spans.At(0)
				attrs := span.Attributes()

				// Verify enrichment flags
				enriched, ok := attrs.Get(enrichedAttribute)
				assert.True(t, ok)
				assert.True(t, enriched.Bool())

				// Verify Cloud Trace attributes
				projectID, ok := attrs.Get(projectAttribute)
				assert.True(t, ok)
				assert.Equal(t, "test-project", projectID.Str())

				// Verify enriched labels
				region, ok := attrs.Get("cloudtrace.label.cloud.region")
				assert.True(t, ok)
				assert.Equal(t, "us-central1", region.Str())
			},
		},
		{
			name: "Process traces with x-cloud-trace-context",
			config: &Config{
				ProjectID:         "test-project",
				PreferTraceparent: false,
				CacheSize:         1000,
				CacheTTL:          5 * time.Minute,
			},
			mockResponse: loadCloudTraceResponse(),
			validateFunc: func(t *testing.T, traces ptrace.Traces) {
				// Validate that spans were enriched with Cloud Trace data
				rspans := traces.ResourceSpans()
				require.Equal(t, 1, rspans.Len())

				spans := rspans.At(0).ScopeSpans().At(0).Spans()
				require.Equal(t, 2, spans.Len())

				// Check second span (x-cloud-trace-context)
				span := spans.At(1)
				attrs := span.Attributes()

				// Verify enrichment flags
				enriched, ok := attrs.Get(enrichedAttribute)
				assert.True(t, ok)
				assert.True(t, enriched.Bool())

				// Verify Cloud Trace attributes
				projectID, ok := attrs.Get(projectAttribute)
				assert.True(t, ok)
				assert.Equal(t, "test-project", projectID.Str())

				// Verify enriched labels
				region, ok := attrs.Get("cloudtrace.label.cloud.region")
				assert.True(t, ok)
				assert.Equal(t, "us-central1", region.Str())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zap.NewNop()
			nextConsumer := consumertest.NewNop()

			// Create processor with mock client
			mockClient := &mockCloudTraceClient{
				mockResponse: tt.mockResponse,
			}
			processor := newProcessorWithClient(logger, tt.config, nextConsumer, mockClient)
			require.NotNil(t, processor)

			// Start the processor
			err := processor.Start(context.Background(), componenttest.NewNopHost())
			require.NoError(t, err)

			// Load test traces
			traces := loadTestTraces(t)

			// Process traces
			err = processor.ConsumeTraces(context.Background(), traces)
			assert.NoError(t, err)

			// Validate results
			if tt.validateFunc != nil {
				tt.validateFunc(t, traces)
			}

			// Shutdown
			err = processor.Shutdown(context.Background())
			assert.NoError(t, err)
		})
	}
}

// newProcessorWithClient creates a new processor with the given client for testing
func newProcessorWithClient(logger *zap.Logger, config *Config, nextConsumer consumer.Traces, client traceClient) *cloudTraceProcessor {
	p := &cloudTraceProcessor{
		logger:       logger,
		config:       config,
		nextConsumer: nextConsumer,
		client:       client,
		cache:        lru.New(config.CacheSize),
	}
	return p
}
