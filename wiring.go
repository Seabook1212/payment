package payment

import (
	"context"
	"net/http"
	"os"

	"github.com/go-kit/kit/log"
	stdopentracing "github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/weaveworks/common/middleware"
)

var (
	HTTPLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "Time (in seconds) spent serving HTTP requests.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path", "status_code", "isWS"})

	HTTPInflightRequests = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "http_inflight_requests",
		Help: "Current number of inflight requests.",
	}, []string{"method", "path"})

	HTTPRequestBodySize = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_body_size_bytes",
		Help:    "Size of HTTP request bodies.",
		Buckets: prometheus.ExponentialBuckets(100, 10, 7),
	}, []string{"method", "path"})

	HTTPResponseBodySize = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_response_body_size_bytes",
		Help:    "Size of HTTP response bodies.",
		Buckets: prometheus.ExponentialBuckets(100, 10, 7),
	}, []string{"method", "path"})
)

func init() {
	prometheus.MustRegister(HTTPLatency)
	prometheus.MustRegister(HTTPInflightRequests)
	prometheus.MustRegister(HTTPRequestBodySize)
	prometheus.MustRegister(HTTPResponseBodySize)
}

func WireUp(ctx context.Context, declineAmount float32, tracer stdopentracing.Tracer, serviceName string) (http.Handler, log.Logger) {
	// Log domain.
	var logger log.Logger
	{
		logger = log.NewLogfmtLogger(os.Stderr)
		logger = log.With(logger, "ts", log.DefaultTimestampUTC)
		logger = log.With(logger, "caller", log.DefaultCaller)
	}

	// Service domain.
	var service Service
	{
		service = NewAuthorisationService(declineAmount)
		// Removed service-level logging - now done at endpoint level with trace context
	}

	// Endpoint domain.
	endpoints := MakeEndpoints(service, tracer, logger)

	router := MakeHTTPHandler(ctx, endpoints, logger, tracer)

	httpMiddleware := []middleware.Interface{
		middleware.Instrument{
			Duration:         HTTPLatency,
			RouteMatcher:     router,
			InflightRequests: HTTPInflightRequests,
			RequestBodySize:  HTTPRequestBodySize,
			ResponseBodySize: HTTPResponseBodySize,
		},
	}

	// Handler
	handler := middleware.Merge(httpMiddleware...).Wrap(router)

	return handler, logger
}
