// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package googlecloudtraceprocessor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
)

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

	// Create test traces
	traces := ptrace.NewTraces()
	err = processor.ConsumeTraces(context.Background(), traces)
	assert.NoError(t, err)

	// Test shutdown
	err = processor.Shutdown(context.Background())
	assert.NoError(t, err)
}
