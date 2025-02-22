# Google Cloud Trace Processor

The Google Cloud Trace processor is designed to enrich trace data by retrieving additional details from the Google Cloud Trace API. It examines incoming spans for trace context headers—specifically `traceparent` and `X-Cloud-Trace-Context`—and uses the associated information to pull external data for enrichment.

## Configuration

The following configuration options are available:

- `project_id` (required): The Google Cloud Project ID where the traces will be queried from.
- `credentials_file` (optional): Path to the Google Cloud credentials JSON file. If not specified, default credentials will be used.
- `prefer_traceparent` (optional, default: true): Whether to prefer W3C traceparent header over X-Cloud-Trace-Context.
- `cache_size` (optional, default: 1000): Maximum number of trace IDs to cache.
- `cache_ttl` (optional, default: 5m): Time-to-live for cache entries.

Example configuration:

```yaml
processors:
  googlecloudtrace:
    project_id: my-gcp-project
    credentials_file: /path/to/credentials.json
    prefer_traceparent: true
    cache_size: 2000
    cache_ttl: 10m
```

## Metrics

The following metrics are emitted by the processor:

| Metric                                | Type    | Description                                  |
| ------------------------------------- | ------- | -------------------------------------------- |
| `googlecloudtrace.api_calls`          | Counter | Number of Cloud Trace API calls made         |
| `googlecloudtrace.spans_enriched`     | Counter | Number of spans successfully enriched        |
| `googlecloudtrace.api_errors`         | Counter | Number of Cloud Trace API errors encountered |
| `googlecloudtrace.enrichment_latency` | Gauge   | Latency of enrichment operations             |

## Example Pipeline

Here's an example pipeline configuration that uses the Google Cloud Trace processor along with the OpenTelemetry Collector's batch processor:

```yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: localhost:4317

processors:
  batch:
    timeout: 10s
    send_batch_size: 100
  googlecloudtrace:
    project_id: my-gcp-project
    prefer_traceparent: true
    cache_size: 1000
    cache_ttl: 5m

exporters:
  otlp:
    endpoint: localhost:4318
    tls:
      insecure: true

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch, googlecloudtrace]
      exporters: [otlp]
```

## Development Status

This processor is currently in the development stage. The implementation is not yet complete and may undergo significant changes.
