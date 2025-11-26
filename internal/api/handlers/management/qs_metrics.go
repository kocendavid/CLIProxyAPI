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
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, metricsUIHTML)
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

const metricsUIHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Usage Metrics Dashboard</title>
    <script src="https://cdn.jsdelivr.net/npm/chart.js@4.4.0/dist/chart.umd.min.js"></script>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: #333;
            padding: 20px;
            min-height: 100vh;
        }
        
        .container {
            max-width: 1400px;
            margin: 0 auto;
        }
        
        header {
            background: white;
            padding: 30px;
            border-radius: 10px;
            box-shadow: 0 4px 6px rgba(0,0,0,0.1);
            margin-bottom: 30px;
        }
        
        h1 {
            color: #667eea;
            font-size: 2.5em;
            margin-bottom: 10px;
        }
        
        .subtitle {
            color: #666;
            font-size: 1.1em;
        }
        
        .kpi-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(250px, 1fr));
            gap: 20px;
            margin-bottom: 30px;
        }
        
        .kpi-card {
            background: white;
            padding: 30px;
            border-radius: 10px;
            box-shadow: 0 4px 6px rgba(0,0,0,0.1);
            text-align: center;
            transition: transform 0.2s;
        }
        
        .kpi-card:hover {
            transform: translateY(-5px);
        }
        
        .kpi-label {
            font-size: 0.9em;
            color: #666;
            text-transform: uppercase;
            letter-spacing: 1px;
            margin-bottom: 10px;
        }
        
        .kpi-value {
            font-size: 3em;
            font-weight: bold;
            color: #667eea;
        }
        
        .charts-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(500px, 1fr));
            gap: 30px;
            margin-bottom: 30px;
        }
        
        .chart-card {
            background: white;
            padding: 30px;
            border-radius: 10px;
            box-shadow: 0 4px 6px rgba(0,0,0,0.1);
        }
        
        .chart-title {
            font-size: 1.5em;
            margin-bottom: 20px;
            color: #333;
        }
        
        .chart-container {
            position: relative;
            height: 400px;
        }
        
        .filters {
            background: white;
            padding: 20px;
            border-radius: 10px;
            box-shadow: 0 4px 6px rgba(0,0,0,0.1);
            margin-bottom: 30px;
            display: flex;
            gap: 20px;
            flex-wrap: wrap;
            align-items: center;
        }
        
        .filter-group {
            display: flex;
            flex-direction: column;
            gap: 5px;
        }
        
        .filter-group label {
            font-size: 0.9em;
            color: #666;
            font-weight: 500;
        }
        
        .filter-group input,
        .filter-group select {
            padding: 8px 12px;
            border: 2px solid #e0e0e0;
            border-radius: 5px;
            font-size: 1em;
            transition: border-color 0.2s;
        }
        
        .filter-group input:focus,
        .filter-group select:focus {
            outline: none;
            border-color: #667eea;
        }
        
        button {
            padding: 10px 20px;
            background: #667eea;
            color: white;
            border: none;
            border-radius: 5px;
            font-size: 1em;
            cursor: pointer;
            transition: background 0.2s;
            align-self: flex-end;
        }
        
        button:hover {
            background: #5568d3;
        }
        
        .status {
            text-align: center;
            color: white;
            font-size: 0.9em;
            margin-top: 20px;
            opacity: 0.8;
        }
        
        .error {
            background: #ff4444;
            color: white;
            padding: 15px;
            border-radius: 5px;
            margin-bottom: 20px;
            display: none;
        }
        
        @media (max-width: 768px) {
            .charts-grid {
                grid-template-columns: 1fr;
            }
            
            h1 {
                font-size: 2em;
            }
            
            .kpi-value {
                font-size: 2em;
            }
        }
    </style>
</head>
<body>
    <div class="container">
        <header>
            <h1>📊 Usage Metrics Dashboard</h1>
            <p class="subtitle">Real-time monitoring of API usage and token consumption</p>
        </header>
        
        <div class="error" id="error"></div>
        
        <div class="filters">
            <div class="filter-group">
                <label for="timeRange">Time Range</label>
                <select id="timeRange">
                    <option value="1">Last Hour</option>
                    <option value="6">Last 6 Hours</option>
                    <option value="24" selected>Last 24 Hours</option>
                    <option value="168">Last 7 Days</option>
                    <option value="720">Last 30 Days</option>
                    <option value="custom">Custom Range</option>
                </select>
            </div>
            
            <div class="filter-group" id="customFromGroup" style="display: none;">
                <label for="customFrom">From</label>
                <input type="datetime-local" id="customFrom">
            </div>
            
            <div class="filter-group" id="customToGroup" style="display: none;">
                <label for="customTo">To</label>
                <input type="datetime-local" id="customTo">
            </div>
            
            <button onclick="loadMetrics()">Refresh</button>
        </div>
        
        <div class="kpi-grid">
            <div class="kpi-card">
                <div class="kpi-label">Total Requests</div>
                <div class="kpi-value" id="totalRequests">-</div>
            </div>
            <div class="kpi-card">
                <div class="kpi-label">Total Tokens</div>
                <div class="kpi-value" id="totalTokens">-</div>
            </div>
            <div class="kpi-card">
                <div class="kpi-label">Avg Tokens/Request</div>
                <div class="kpi-value" id="avgTokens">-</div>
            </div>
        </div>
        
        <div class="charts-grid">
            <div class="chart-card">
                <h2 class="chart-title">Requests Over Time</h2>
                <div class="chart-container">
                    <canvas id="timeseriesChart"></canvas>
                </div>
            </div>
            
            <div class="chart-card">
                <h2 class="chart-title">Usage by Model</h2>
                <div class="chart-container">
                    <canvas id="modelChart"></canvas>
                </div>
            </div>
        </div>
        
        <div class="status" id="status">Last updated: Never</div>
    </div>
    
    <script>
        let timeseriesChart = null;
        let modelChart = null;
        let autoRefreshInterval = null;
        
        // Get management key from URL or prompt
        function getManagementKey() {
            const params = new URLSearchParams(window.location.search);
            return params.get('key') || prompt('Enter management key:');
        }
        
        // Format number with commas
        function formatNumber(num) {
            return num.toString().replace(/\B(?=(\d{3})+(?!\d))/g, ',');
        }
        
        // Show error message
        function showError(message) {
            const errorEl = document.getElementById('error');
            errorEl.textContent = message;
            errorEl.style.display = 'block';
            setTimeout(() => {
                errorEl.style.display = 'none';
            }, 5000);
        }
        
        // Build API URL with filters
        function buildMetricsURL() {
            const timeRange = document.getElementById('timeRange').value;
            const baseURL = window.location.pathname.replace('/ui', '');
            const params = new URLSearchParams();
            
            if (timeRange === 'custom') {
                const from = document.getElementById('customFrom').value;
                const to = document.getElementById('customTo').value;
                if (from) params.append('from', new Date(from).toISOString());
                if (to) params.append('to', new Date(to).toISOString());
            } else {
                const hours = parseInt(timeRange);
                const to = new Date();
                const from = new Date(to.getTime() - hours * 60 * 60 * 1000);
                params.append('from', from.toISOString());
                params.append('to', to.toISOString());
            }
            
            return baseURL + (params.toString() ? '?' + params.toString() : '');
        }
        
        // Load metrics from API
        async function loadMetrics() {
            const key = getManagementKey();
            if (!key) return;
            
            try {
                const url = buildMetricsURL();
                const response = await fetch(url, {
                    headers: {
                        'X-Management-Key': key
                    }
                });
                
                if (!response.ok) {
                    const error = await response.json();
                    throw new Error(error.error || 'Failed to load metrics');
                }
                
                const data = await response.json();
                updateDashboard(data);
                
                document.getElementById('status').textContent = 
                    'Last updated: ' + new Date().toLocaleTimeString();
                    
            } catch (error) {
                console.error('Error loading metrics:', error);
                showError(error.message);
            }
        }
        
        // Update dashboard with new data
        function updateDashboard(data) {
            // Update KPIs
            document.getElementById('totalRequests').textContent = 
                formatNumber(data.totals.requests);
            document.getElementById('totalTokens').textContent = 
                formatNumber(data.totals.tokens);
            document.getElementById('avgTokens').textContent = 
                data.totals.requests > 0 
                    ? formatNumber(Math.round(data.totals.tokens / data.totals.requests))
                    : '0';
            
            // Update timeseries chart
            updateTimeseriesChart(data.timeseries);
            
            // Update model chart
            updateModelChart(data.by_model);
        }
        
        // Update timeseries chart
        function updateTimeseriesChart(timeseries) {
            const ctx = document.getElementById('timeseriesChart');
            
            const labels = timeseries.map(t => {
                const date = new Date(t.bucket_start);
                return date.toLocaleString('en-US', { 
                    month: 'short', 
                    day: 'numeric', 
                    hour: '2-digit',
                    minute: '2-digit'
                });
            });
            
            const requestData = timeseries.map(t => t.requests);
            const tokenData = timeseries.map(t => t.tokens / 1000); // Show in thousands
            
            if (timeseriesChart) {
                timeseriesChart.data.labels = labels;
                timeseriesChart.data.datasets[0].data = requestData;
                timeseriesChart.data.datasets[1].data = tokenData;
                timeseriesChart.update();
            } else {
                timeseriesChart = new Chart(ctx, {
                    type: 'line',
                    data: {
                        labels: labels,
                        datasets: [
                            {
                                label: 'Requests',
                                data: requestData,
                                borderColor: '#667eea',
                                backgroundColor: 'rgba(102, 126, 234, 0.1)',
                                tension: 0.4,
                                fill: true,
                                yAxisID: 'y'
                            },
                            {
                                label: 'Tokens (K)',
                                data: tokenData,
                                borderColor: '#764ba2',
                                backgroundColor: 'rgba(118, 75, 162, 0.1)',
                                tension: 0.4,
                                fill: true,
                                yAxisID: 'y1'
                            }
                        ]
                    },
                    options: {
                        responsive: true,
                        maintainAspectRatio: false,
                        interaction: {
                            mode: 'index',
                            intersect: false,
                        },
                        plugins: {
                            legend: {
                                position: 'top',
                            }
                        },
                        scales: {
                            y: {
                                type: 'linear',
                                position: 'left',
                                title: {
                                    display: true,
                                    text: 'Requests'
                                }
                            },
                            y1: {
                                type: 'linear',
                                position: 'right',
                                title: {
                                    display: true,
                                    text: 'Tokens (K)'
                                },
                                grid: {
                                    drawOnChartArea: false
                                }
                            }
                        }
                    }
                });
            }
        }
        
        // Update model chart
        function updateModelChart(byModel) {
            const ctx = document.getElementById('modelChart');
            
            const labels = byModel.map(m => m.model);
            const tokenData = byModel.map(m => m.tokens);
            const requestData = byModel.map(m => m.requests);
            
            // Generate colors
            const colors = [
                '#667eea', '#764ba2', '#f093fb', '#4facfe',
                '#43e97b', '#fa709a', '#30cfd0', '#a8edea'
            ];
            
            if (modelChart) {
                modelChart.data.labels = labels;
                modelChart.data.datasets[0].data = tokenData;
                modelChart.update();
            } else {
                modelChart = new Chart(ctx, {
                    type: 'bar',
                    data: {
                        labels: labels,
                        datasets: [{
                            label: 'Tokens',
                            data: tokenData,
                            backgroundColor: colors,
                            borderColor: colors.map(c => c),
                            borderWidth: 2
                        }]
                    },
                    options: {
                        responsive: true,
                        maintainAspectRatio: false,
                        plugins: {
                            legend: {
                                display: false
                            },
                            tooltip: {
                                callbacks: {
                                    afterLabel: function(context) {
                                        const idx = context.dataIndex;
                                        return 'Requests: ' + formatNumber(requestData[idx]);
                                    }
                                }
                            }
                        },
                        scales: {
                            y: {
                                beginAtZero: true,
                                title: {
                                    display: true,
                                    text: 'Tokens'
                                },
                                ticks: {
                                    callback: function(value) {
                                        return formatNumber(value);
                                    }
                                }
                            }
                        }
                    }
                });
            }
        }
        
        // Handle time range selection
        document.getElementById('timeRange').addEventListener('change', function() {
            const isCustom = this.value === 'custom';
            document.getElementById('customFromGroup').style.display = isCustom ? 'flex' : 'none';
            document.getElementById('customToGroup').style.display = isCustom ? 'flex' : 'none';
            
            if (!isCustom) {
                loadMetrics();
            }
        });
        
        // Start auto-refresh
        function startAutoRefresh() {
            if (autoRefreshInterval) {
                clearInterval(autoRefreshInterval);
            }
            autoRefreshInterval = setInterval(loadMetrics, 30000); // 30 seconds
        }
        
        // Initialize
        document.addEventListener('DOMContentLoaded', function() {
            loadMetrics();
            startAutoRefresh();
        });
    </script>
</body>
</html>`

