// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package googlecloudtraceprocessor

import (
	"fmt"
	"time"

	"go.opentelemetry.io/collector/component"
)

// Config defines configuration for the Cloud Trace Enrichment processor.
type Config struct {
	// ProjectID is the Google Cloud Project ID where the traces will be queried from.
	ProjectID string `mapstructure:"project_id"`

	// CredentialsFile is the path to the Google Cloud credentials JSON file.
	// If not specified, default credentials will be used.
	CredentialsFile string `mapstructure:"credentials_file"`

	// PreferTraceparent determines whether to prefer W3C traceparent header over X-Cloud-Trace-Context.
	// Default is true.
	PreferTraceparent bool `mapstructure:"prefer_traceparent"`

	// CacheSize is the maximum number of trace IDs to cache.
	CacheSize int `mapstructure:"cache_size"`

	// CacheTTL is the time-to-live for cache entries.
	CacheTTL time.Duration `mapstructure:"cache_ttl"`
}

// Validate checks if the processor configuration is valid
func (cfg *Config) Validate() error {
	if cfg.ProjectID == "" {
		return fmt.Errorf("project_id cannot be empty")
	}

	if cfg.CacheSize <= 0 {
		return fmt.Errorf("cache_size must be greater than 0")
	}

	if cfg.CacheTTL <= 0 {
		return fmt.Errorf("cache_ttl must be greater than 0")
	}

	return nil
}

var _ component.Config = (*Config)(nil)
