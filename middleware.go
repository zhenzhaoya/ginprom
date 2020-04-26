package ginprom

import (
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
)

const namespace = "service"

var (
	labels = []string{"status", "endpoint", "method"}

	uptime = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "uptime",
			Help:      "HTTP service uptime.",
		}, nil,
	)

	reqCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "http_request_count_total",
			Help:      "Total number of HTTP requests made.",
		}, labels,
	)

	reqDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "http_request_duration_seconds",
			Help:      "HTTP request latencies in seconds.",
		}, labels,
	)

	reqSizeBytes = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace: namespace,
			Name:      "http_request_size_bytes",
			Help:      "HTTP request sizes in bytes.",
		}, labels,
	)

	respSizeBytes = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace: namespace,
			Name:      "http_response_size_bytes",
			Help:      "HTTP request sizes in bytes.",
		}, labels,
	)
)

// init registers the prometheus metrics
func init() {
	prometheus.MustRegister(uptime, reqCount, reqDuration, reqSizeBytes, respSizeBytes)
	go recordUptime()
}

// recordUptime increases service uptime per second.
func recordUptime() {
	for range time.Tick(time.Second) {
		uptime.WithLabelValues().Inc()
	}
}

// calcRequestSize returns the size of request object.
func calcRequestSize(r *http.Request) float64 {
	size := 0
	if r.URL != nil {
		size = len(r.URL.String())
	}

	size += len(r.Method)
	size += len(r.Proto)

	for name, values := range r.Header {
		size += len(name)
		for _, value := range values {
			size += len(value)
		}
	}
	size += len(r.Host)

	// r.Form and r.MultipartForm are assumed to be included in r.URL.
	if r.ContentLength != -1 {
		size += int(r.ContentLength)
	}
	return float64(size)
}

// PromOpts represents the Prometheus middleware Options.
// It is used for filtering labels by regex.
type PromOpts struct {
	ExcludeRegexStatus   string
	ExcludeRegexEndpoint string
	ExcludeRegexMethod   string
}
type PromOptsReg struct {
	ExcludeRegexStatus   *regexp.Regexp
	ExcludeRegexEndpoint *regexp.Regexp
	ExcludeRegexMethod   *regexp.Regexp
}

var defaultPromOpts = &PromOpts{}

// checkLabel returns the match result of labels.
// Return true if regex-pattern compiles failed.
func (po *PromOptsReg) checkLabel(label, pattern *regexp.Regexp) bool {
	if pattern == nil {
		return true
	}

	matched, err := pattern.MatchString(label)
	if err != nil {
		return true
	}
	return !matched
}

// PromMiddleware returns a gin.HandlerFunc for exporting some Web metrics
func PromMiddleware(promOpts *PromOpts) gin.HandlerFunc {
	// make sure promOpts is not nil
	promRegs := &PromOptsReg{}
	if promOpts != nil {
		if promOpts.ExcludeRegexEndpoint != "" {
			promRegs.ExcludeRegexStatus = regexp.MustCompile(promOpts.ExcludeRegexStatus)
		}
		if promOpts.ExcludeRegexMethod != "" {
			promRegs.ExcludeRegexMethod = regexp.MustCompile(promOpts.ExcludeRegexMethod)
		}
		if promOpts.ExcludeRegexEndpoint != "" {
			promRegs.ExcludeRegexEndpoint = regexp.MustCompile(promOpts.ExcludeRegexEndpoint)
		}
	}

	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		status := fmt.Sprintf("%d", c.Writer.Status())
		endpoint := c.Request.URL.Path
		method := c.Request.Method

		lvs := []string{status, endpoint, method}

		isOk := promRegs.checkLabel(status, promRegs.ExcludeRegexStatus) &&
			promRegs.checkLabel(endpoint, promRegs.ExcludeRegexEndpoint) &&
			promRegs.checkLabel(method, promRegs.ExcludeRegexMethod)

		if !isOk {
			return
		}

		reqCount.WithLabelValues(lvs...).Inc()
		reqDuration.WithLabelValues(lvs...).Observe(time.Since(start).Seconds())
		reqSizeBytes.WithLabelValues(lvs...).Observe(calcRequestSize(c.Request))
		respSizeBytes.WithLabelValues(lvs...).Observe(float64(c.Writer.Size()))
	}
}

// PromHandler wrappers the standard http.Handler to gin.HandlerFunc
func PromHandler(handler http.Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		handler.ServeHTTP(c.Writer, c.Request)
	}
}
