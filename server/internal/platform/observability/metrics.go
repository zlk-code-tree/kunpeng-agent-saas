package observability

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

type routeMetric struct {
	Requests      uint64
	Errors        uint64
	DurationNanos uint64
}

type Metrics struct {
	active atomic.Int64
	mu     sync.RWMutex
	routes map[string]*routeMetric
}

// NewMetrics 创建进程内 HTTP 指标采集器。
func NewMetrics() *Metrics { return &Metrics{routes: make(map[string]*routeMetric)} }

// Middleware 按请求方法和路由模板统计请求数、服务端错误数和总耗时。
func (m *Metrics) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		m.active.Add(1)
		defer m.active.Add(-1)
		c.Next()
		// 使用 Gin 路由模板而不是原始 URL，限制标签基数，避免每个资源 ID 生成独立指标序列。
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		key := c.Request.Method + " " + route
		m.mu.Lock()
		metric := m.routes[key]
		if metric == nil {
			metric = &routeMetric{}
			m.routes[key] = metric
		}
		metric.Requests++
		if c.Writer.Status() >= http.StatusInternalServerError {
			metric.Errors++
		}
		metric.DurationNanos += uint64(time.Since(started))
		m.mu.Unlock()
	}
}

// Handler 以 Prometheus 文本格式暴露当前进程采集到的指标。
func (m *Metrics) Handler(c *gin.Context) {
	// 指标只属于当前进程；多副本部署应由 Prometheus 抓取全部实例并按路由聚合，无需进程间同步。
	var output strings.Builder
	output.WriteString("# HELP gin_dify_http_active_requests Current active HTTP requests.\n")
	output.WriteString("# TYPE gin_dify_http_active_requests gauge\n")
	output.WriteString("gin_dify_http_active_requests ")
	output.WriteString(strconv.FormatInt(m.active.Load(), 10))
	output.WriteByte('\n')
	output.WriteString("# HELP gin_dify_http_requests_total HTTP requests by route.\n")
	output.WriteString("# TYPE gin_dify_http_requests_total counter\n")
	m.mu.RLock()
	defer m.mu.RUnlock()
	for key, metric := range m.routes {
		parts := strings.SplitN(key, " ", 2)
		method, route := parts[0], parts[1]
		labels := fmt.Sprintf("method=%q,route=%q", method, route)
		fmt.Fprintf(&output, "gin_dify_http_requests_total{%s} %d\n", labels, metric.Requests)
		fmt.Fprintf(&output, "gin_dify_http_errors_total{%s} %d\n", labels, metric.Errors)
		fmt.Fprintf(&output, "gin_dify_http_request_duration_seconds_sum{%s} %.6f\n", labels, float64(metric.DurationNanos)/float64(time.Second))
	}
	c.Data(http.StatusOK, "text/plain; version=0.0.4; charset=utf-8", []byte(output.String()))
}
