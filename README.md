# Outline Wiki Exporter

[![Docker Hub](https://img.shields.io/badge/Docker-dwesh163%2Foutlinewiki--exporter-blue)](https://hub.docker.com/r/dwesh163/outlinewiki-exporter)

A Prometheus exporter for Outline Wiki that monitors and exposes metrics from your Outline instance.

## Description

This tool collects metrics from your Outline Wiki instance via its API and exposes them in Prometheus format.

## Quick Start

```bash
docker run -d --name outline-exporter \
  -e OUTLINE_API_URL="https://your-outline-instance.com" \
  -e OUTLINE_API_KEY="your-outline-api-key" \
  -p 9877:9877 \
  dwesh163/outlinewiki-exporter
```

## Configuration

### Environment Variables

| Variable          | Description                                      | Default                 | Example                            |
| ----------------- | ------------------------------------------------ | ----------------------- | ---------------------------------- |
| `OUTLINE_API_URL` | URL of your Outline instance                     | `http://localhost:3000` | `https://docs.company.com`         |
| `OUTLINE_API_KEY` | Your Outline API key (**required**)              | -                       | `ol_api_xxxxxxxxxxxxx`             |
| `LISTEN_ADDRESS`  | Address for the exporter to listen on            | `:9877`                 | `:8080` or `0.0.0.0:9877`          |
| `METRICS_PATH`    | Path to access metrics                           | `/metrics`              | `/prometheus` or `/metrics`        |
| `SCRAPE_TIMEOUT`  | Timeout for API requests                         | `10s`                   | `30s`, `1m`, `500ms`               |
| `PAGE_LIMIT`      | Number of items per page for API pagination      | `100`                    | `50`, `100`                        |
| `DEBUG`           | Enable debug logging (shows API requests/responses) | `false`              | `true`, `1`, `yes`                 |

### Running Locally

```bash
# Set environment variables
export OUTLINE_API_URL="https://your-outline.com"
export OUTLINE_API_KEY="ol_api_your_key_here"
export DEBUG="true"

# Run the exporter
go run main.go
```

### Docker Compose Example

```yaml
version: '3.8'
services:
  outline-exporter:
    image: dwesh163/outlinewiki-exporter
    environment:
      OUTLINE_API_URL: "https://your-outline.com"
      OUTLINE_API_KEY: "ol_api_your_key_here"
      LISTEN_ADDRESS: ":9877"
      METRICS_PATH: "/metrics"
      SCRAPE_TIMEOUT: "30s"
      PAGE_LIMIT: "50"
      DEBUG: "false"
    ports:
      - "9877:9877"
    restart: unless-stopped
```

### Prometheus Configuration

```yaml
scrape_configs:
  - job_name: 'outline'
    static_configs:
      - targets: ['outline-exporter:9877']
    scrape_interval: 60s
    scrape_timeout: 30s
```

## Complete List of Metrics

### Status Metrics

-   `outline_up` - Whether the last scrape was successful (1 = success, 0 = error)
-   `outline_scrape_success_timestamp` - Timestamp of the last successful scrape
-   `outline_scrape_errors_total` - Total number of scrape errors
-   `outline_scrape_duration_seconds` - Duration of the scrape operation

### Collection Metrics

-   `outline_collections_total` - Total number of collections
-   `outline_collection_documents_count` - Number of documents in a collection (labels: collection_id, collection_name)
-   `outline_collection_age_seconds` - Age of a collection in seconds (labels: collection_id, collection_name)

### Document Metrics

-   `outline_documents_total` - Total number of documents
-   `outline_document_revisions` - Number of revisions for a document (labels: document_id, collection_id)
-   `outline_document_views` - Number of views for a document (labels: document_id, collection_id)
-   `outline_document_age_seconds` - Age of document in seconds (labels: document_id, collection_id)
-   `outline_document_size_bytes` - Size of document text in bytes (labels: document_id, collection_id)
-   `outline_document_update_age_seconds` - Time since last document update in seconds (labels: document_id, collection_id)

### User Metrics

-   `outline_users_total` - Total number of users
-   `outline_user_last_active_seconds` - Time since user was last active in seconds (labels: user_id, user_name)
-   `outline_user_age_seconds` - Age of user account in seconds (labels: user_id, user_name)

## Endpoints

-   `/` - Home page with link to metrics
-   `/metrics` - Prometheus metrics endpoint (configurable via `METRICS_PATH`)
-   `/healthz` - Health check endpoint (returns `OK`)

## Building from Source

```bash
git clone https://github.com/dwesh163/outlinewiki-exporter.git
cd outlinewiki-exporter
go build -o outline-exporter
./outline-exporter
```

## Getting Your Outline API Key

1. Log in to your Outline instance
2. Go to Settings → API
3. Create a new API token
4. Copy the token (starts with `ol_api_`)

## Troubleshooting

### Enable Debug Mode

Set `DEBUG=true` to see detailed API requests and responses:

```bash
DEBUG=true go run main.go
```

### Common Issues

**"OUTLINE_API_KEY environment variable is required"** - Make sure you've set the `OUTLINE_API_KEY` environment variable

**Timeout errors** - Increase `SCRAPE_TIMEOUT` if you have a large Outline instance:
```bash
SCRAPE_TIMEOUT=30s go run main.go
```

**Duplicate metrics** - The exporter automatically handles pagination and deduplicates documents to prevent duplicate metrics
