// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package googlecloudtraceprocessor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		shouldError bool
	}{
		{
			name: "Valid config",
			config: Config{
				ProjectID:         "test-project",
				PreferTraceparent: true,
				CacheSize:         1000,
				CacheTTL:          5 * time.Minute,
			},
			shouldError: false,
		},
		{
			name: "Missing project ID",
			config: Config{
				PreferTraceparent: true,
				CacheSize:         1000,
				CacheTTL:          5 * time.Minute,
			},
			shouldError: true,
		},
		{
			name: "Invalid cache size",
			config: Config{
				ProjectID:         "test-project",
				PreferTraceparent: true,
				CacheSize:         0,
				CacheTTL:          5 * time.Minute,
			},
			shouldError: true,
		},
		{
			name: "Invalid cache TTL",
			config: Config{
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
			err := tt.config.Validate()
			if tt.shouldError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
