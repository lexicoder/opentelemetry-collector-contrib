// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package googlecloudtraceprocessor

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
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
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
)

type testTrace struct {
	ResourceSpans []struct {
		Resource struct {
			Attributes []struct {
				Key   string `json:"key"`
				Value struct {
					StringValue string `json:"stringValue"`
				} `json:"value"`
			} `json:"attributes"`
		} `json:"resource"`
		ScopeSpans []struct {
			Scope struct {
				Name string `json:"name"`
			} `json:"scope"`
			Spans []struct {
				TraceID           string `json:"traceId"`
				SpanID            string `json:"spanId"`
				Name              string `json:"name"`
				Kind              int32  `json:"kind"`
				StartTimeUnixNano string `json:"startTimeUnixNano"`
				EndTimeUnixNano   string `json:"endTimeUnixNano"`
				Attributes        []struct {
					Key   string `json:"key"`
					Value struct {
						StringValue string `json:"stringValue"`
					} `json:"value"`
				} `json:"attributes"`
			} `json:"spans"`
		} `json:"scopeSpans"`
	} `json:"resourceSpans"`
}

type mockCloudTraceClient struct {
	mockResponse *tracepb.Trace
}

func (m *mockCloudTraceClient) GetTrace(ctx context.Context, projectID, traceID string) (*tracepb.Trace, error) {
	if m.mockResponse != nil {
		return m.mockResponse, nil
	}
	return nil, nil
}

func (m *mockCloudTraceClient) Close() error {
	return nil
}

func loadTestData(t *testing.T, filename string) []byte {
	data, err := os.ReadFile(filepath.Join("testdata", filename))
	require.NoError(t, err)
	return data
}

func loadTestTraces(t *testing.T) ptrace.Traces {
	data := loadTestData(t, "traces.json")
	var testData testTrace
	err := json.Unmarshal(data, &testData)
	require.NoError(t, err)

	traces := ptrace.NewTraces()

	for _, rs := range testData.ResourceSpans {
		resourceSpans := traces.ResourceSpans().AppendEmpty()

		// Set resource attributes
		for _, attr := range rs.Resource.Attributes {
			resourceSpans.Resource().Attributes().PutStr(attr.Key, attr.Value.StringValue)
		}

		for _, ss := range rs.ScopeSpans {
			scopeSpans := resourceSpans.ScopeSpans().AppendEmpty()
			scopeSpans.Scope().SetName(ss.Scope.Name)

			for _, span := range ss.Spans {
				spanData := scopeSpans.Spans().AppendEmpty()

				// Convert trace ID from hex string to bytes
				traceID, err := hex.DecodeString(span.TraceID)
				require.NoError(t, err)
				spanData.SetTraceID(pcommon.TraceID(traceID))

				// Convert span ID from hex string to bytes
				spanID, err := hex.DecodeString(span.SpanID)
				require.NoError(t, err)
				spanData.SetSpanID(pcommon.SpanID(spanID))

				spanData.SetName(span.Name)
				spanData.SetKind(ptrace.SpanKind(span.Kind))

				// Set start and end time
				startNanos, err := strconv.ParseInt(span.StartTimeUnixNano, 10, 64)
				require.NoError(t, err)
				spanData.SetStartTimestamp(pcommon.Timestamp(startNanos))

				endNanos, err := strconv.ParseInt(span.EndTimeUnixNano, 10, 64)
				require.NoError(t, err)
				spanData.SetEndTimestamp(pcommon.Timestamp(endNanos))

				// Set attributes
				for _, attr := range span.Attributes {
					spanData.Attributes().PutStr(attr.Key, attr.Value.StringValue)
				}
			}
		}
	}

	return traces
}

func loadCloudTraceResponse(t *testing.T) *tracepb.Trace {
	data := loadTestData(t, "cloudtrace_response.json")
	var response struct {
		ProjectID string `json:"projectId"`
		TraceID   string `json:"traceId"`
		Spans     []struct {
			SpanID    string            `json:"spanId"`
			Name      string            `json:"name"`
			StartTime string            `json:"startTime"`
			EndTime   string            `json:"endTime"`
			Labels    map[string]string `json:"labels"`
		} `json:"spans"`
	}
	err := json.Unmarshal(data, &response)
	require.NoError(t, err)

	// Convert to Cloud Trace proto
	trace := &tracepb.Trace{
		ProjectId: response.ProjectID,
		TraceId:   response.TraceID,
		Spans:     make([]*tracepb.TraceSpan, len(response.Spans)),
	}

	for i, span := range response.Spans {
		// Convert span ID from hex to uint64
		spanID, err := strconv.ParseUint(span.SpanID, 16, 64)
		require.NoError(t, err)

		// Parse timestamps
		startTime, err := time.Parse(time.RFC3339, span.StartTime)
		require.NoError(t, err)
		endTime, err := time.Parse(time.RFC3339, span.EndTime)
		require.NoError(t, err)

		trace.Spans[i] = &tracepb.TraceSpan{
			SpanId:    spanID,
			Name:      span.Name,
			StartTime: timestamppb.New(startTime),
			EndTime:   timestamppb.New(endTime),
			Labels:    span.Labels,
		}
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
			mockResponse: loadCloudTraceResponse(t),
			validateFunc: func(t *testing.T, traces ptrace.Traces) {
				// Validate that spans were enriched with Cloud Trace data
				rspans := traces.ResourceSpans()
				require.Equal(t, 1, rspans.Len())

				spans := rspans.At(0).ScopeSpans().At(0).Spans()
				require.Equal(t, 2, spans.Len())

				// Check first span (traceparent)
				span := spans.At(0)
				attrs := span.Attributes()
				val, ok := attrs.Get("cloudtrace.label.cloud.region")
				assert.True(t, ok)
				assert.Equal(t, "us-central1", val.AsString())
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
			mockResponse: loadCloudTraceResponse(t),
			validateFunc: func(t *testing.T, traces ptrace.Traces) {
				// Validate that spans were enriched with Cloud Trace data
				rspans := traces.ResourceSpans()
				require.Equal(t, 1, rspans.Len())

				spans := rspans.At(0).ScopeSpans().At(0).Spans()
				require.Equal(t, 2, spans.Len())

				// Check second span (x-cloud-trace-context)
				span := spans.At(1)
				attrs := span.Attributes()
				val, ok := attrs.Get("cloudtrace.label.cloud.region")
				assert.True(t, ok)
				assert.Equal(t, "us-central1", val.AsString())
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
			processor, err := newProcessorWithClient(logger, tt.config, nextConsumer, mockClient)
			require.NoError(t, err)
			require.NotNil(t, processor)

			// Start the processor
			err = processor.Start(context.Background(), componenttest.NewNopHost())
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
func newProcessorWithClient(logger *zap.Logger, config *Config, nextConsumer consumer.Traces, client traceClient) (*cloudTraceProcessor, error) {
	p := &cloudTraceProcessor{
		logger:       logger,
		config:       config,
		nextConsumer: nextConsumer,
		client:       client,
		cache:        lru.New(config.CacheSize),
	}
	return p, nil
}
