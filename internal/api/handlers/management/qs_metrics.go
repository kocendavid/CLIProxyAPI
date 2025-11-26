// Package management provides the management API handlers and middleware
// for configuring the server and managing auth files.
package management

import (
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

// GetQSHealth returns a simple health check for QuantumSpring metrics endpoints.
// GET /v0/management/qs/health
func (h *Handler) GetQSHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// MetricsResponse represents the aggregated metrics response.
type MetricsResponse struct {
	Totals     MetricsTotals     `json:"totals"`
	ByModel    []ModelMetrics    `json:"by_model"`
	Timeseries []TimeseriesBucket `json:"timeseries"`
}

// MetricsTotals represents overall aggregated metrics.
type MetricsTotals struct {
	Tokens   int64 `json:"tokens"`
	Requests int64 `json:"requests"`
}

// ModelMetrics represents metrics aggregated by model.
type ModelMetrics struct {
	Model    string `json:"model"`
	Tokens   int64  `json:"tokens"`
	Requests int64  `json:"requests"`
}

// TimeseriesBucket represents metrics for a specific time bucket.
type TimeseriesBucket struct {
	BucketStart time.Time `json:"bucket_start"`
	Tokens      int64     `json:"tokens"`
	Requests    int64     `json:"requests"`
}

// GetQSMetrics returns aggregated usage metrics with optional filtering.
// GET /v0/management/qs/metrics?from=2025-11-25T00:00:00Z&to=2025-11-26T00:00:00Z&model=gpt-4
func (h *Handler) GetQSMetrics(c *gin.Context) {
	// Parse query parameters
	fromStr := c.Query("from")
	toStr := c.Query("to")
	modelFilter := c.Query("model")

	// Default time range: last 24 hours
	now := time.Now()
	var fromTime, toTime time.Time
	
	if fromStr != "" {
		var err error
		fromTime, err = time.Parse(time.RFC3339, fromStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid 'from' timestamp format, expected RFC3339"})
			return
		}
	} else {
		fromTime = now.Add(-24 * time.Hour)
	}

	if toStr != "" {
		var err error
		toTime, err = time.Parse(time.RFC3339, toStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid 'to' timestamp format, expected RFC3339"})
			return
		}
	} else {
		toTime = now
	}

	// Validate time range
	if toTime.Before(fromTime) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "'to' must be after 'from'"})
		return
	}

	// Load events from JSON store
	store := h.jsonStore
	if store == nil {
		store = usage.GetJSONStore()
	}

	if store == nil {
		// No store configured, return empty metrics
		c.JSON(http.StatusOK, MetricsResponse{
			Totals:     MetricsTotals{},
			ByModel:    []ModelMetrics{},
			Timeseries: []TimeseriesBucket{},
		})
		return
	}

	events, err := store.Load()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load usage events"})
		return
	}

	// Filter and aggregate events
	response := aggregateMetrics(events, fromTime, toTime, modelFilter)

	c.JSON(http.StatusOK, response)
}

// GetQSMetricsUI serves an HTML dashboard for visualizing usage metrics.
// GET /v0/management/qs/metrics/ui
func (h *Handler) GetQSMetricsUI(c *gin.Context) {
	c.File("static/metrics-dashboard.html")
}

// aggregateMetrics processes events and returns aggregated metrics.
func aggregateMetrics(events []usage.UsageEvent, fromTime, toTime time.Time, modelFilter string) MetricsResponse {
	var totalTokens int64
	var totalRequests int64
	modelStats := make(map[string]*ModelMetrics)
	
	// Timeseries buckets by hour
	hourlyStats := make(map[time.Time]*TimeseriesBucket)

	for _, event := range events {
		// Filter by time range
		if event.Timestamp.Before(fromTime) || event.Timestamp.After(toTime) {
			continue
		}

		// Filter by model if specified
		if modelFilter != "" && event.Model != modelFilter {
			continue
		}

		// Aggregate totals
		totalTokens += event.TotalTokens
		totalRequests++

		// Aggregate by model
		if _, exists := modelStats[event.Model]; !exists {
			modelStats[event.Model] = &ModelMetrics{
				Model:    event.Model,
				Tokens:   0,
				Requests: 0,
			}
		}
		modelStats[event.Model].Tokens += event.TotalTokens
		modelStats[event.Model].Requests++

		// Aggregate by hour
		hourBucket := event.Timestamp.Truncate(time.Hour)
		if _, exists := hourlyStats[hourBucket]; !exists {
			hourlyStats[hourBucket] = &TimeseriesBucket{
				BucketStart: hourBucket,
				Tokens:      0,
				Requests:    0,
			}
		}
		hourlyStats[hourBucket].Tokens += event.TotalTokens
		hourlyStats[hourBucket].Requests++
	}

	// Convert maps to slices for response
	byModel := make([]ModelMetrics, 0, len(modelStats))
	for _, m := range modelStats {
		byModel = append(byModel, *m)
	}

	// Sort by tokens descending
	sort.Slice(byModel, func(i, j int) bool {
		return byModel[i].Tokens > byModel[j].Tokens
	})

	timeseries := make([]TimeseriesBucket, 0, len(hourlyStats))
	for _, bucket := range hourlyStats {
		timeseries = append(timeseries, *bucket)
	}

	// Sort timeseries by timestamp ascending
	sort.Slice(timeseries, func(i, j int) bool {
		return timeseries[i].BucketStart.Before(timeseries[j].BucketStart)
	})

	return MetricsResponse{
		Totals: MetricsTotals{
			Tokens:   totalTokens,
			Requests: totalRequests,
		},
		ByModel:    byModel,
		Timeseries: timeseries,
	}
}

