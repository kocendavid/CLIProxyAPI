# Usage Persistence System - Complete Summary

## Overview
A complete usage tracking and metrics system that persists API usage events to JSON, provides query APIs, and visualizes data through a web dashboard.

## Components

### 1. JSON Storage (`internal/usage/json_store.go`)
- **JSONStore**: Thread-safe event persistence
- **Auto-flush**: 50 events or 30 seconds (whichever comes first)
- **Methods**: `Write()`, `Load()`, `Flush()`, `Close()`
- **Format**: JSON Lines (one event per line)

### 2. Integration (`internal/usage/logger_plugin.go`)
- **Persistence Hook**: Connected to `RequestStatistics.Record()`
- **Async Writing**: Non-blocking background goroutines
- **API Key Hashing**: SHA256 hash (never stores raw keys)
- **Startup Loading**: Historical events loaded on server start
- **File Location**: `~/.cli-proxy-api/usage.json`

### 3. API Endpoints (`internal/api/handlers/management/qs_metrics.go`)
- **`GET /v0/management/qs/health`**: Health check (`{"ok": true}`)
- **`GET /v0/management/qs/metrics`**: Metrics aggregation
  - Query params: `from`, `to`, `model`
  - Returns: `totals`, `by_model`, `timeseries` (hourly buckets)
- **Authentication**: Requires management key
- **Access Control**: Respects `allow-remote-management` setting

### 4. Visualization UI (`/v0/management/qs/metrics/ui`)
- **Dashboard**: Modern HTML/CSS/JS interface with Chart.js
- **KPI Cards**: Total requests, tokens, average tokens/request
- **Charts**:
  - Requests Over Time (dual-axis line chart)
  - Usage by Model (horizontal bar chart)
- **Filters**: Time range presets (hour, 6h, 24h, 7d, 30d, custom)
- **Auto-refresh**: Every 30 seconds
- **Responsive**: Mobile-friendly design

## Data Flow
```
API Request → Record() → Async Write → JSONStore → Disk
                                              ↓
                                         Load on startup
                                              ↓
Browser ← Charts ← JSON API ← Aggregate ← JSON Store
```

## Configuration
- **Enable**: Set `usage-statistics-enabled: true` in config.yaml
- **Storage**: Automatic (auth-dir/usage.json)
- **Access**: Management key required for all endpoints
