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

Set these environment variables:

| Variable          | Description                           | Default                 |
| ----------------- | ------------------------------------- | ----------------------- |
| `OUTLINE_API_URL` | URL of your Outline instance          | `http://localhost:3000` |
| `OUTLINE_API_KEY` | Your Outline API key (required)       | -                       |
| `LISTEN_ADDRESS`  | Address for the exporter to listen on | `:9877`                 |
| `METRICS_PATH`    | Path to access metrics                | `/metrics`              |
| `SCRAPE_TIMEOUT`  | Timeout for API requests              | `5s`                    |
| `DEBUG`           | Enable debug logging                  | `false`                 |

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
