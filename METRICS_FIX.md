# Prometheus Metrics Fix

## Problem
The application was crashing with nil pointer dereference errors when handling HTTP requests:
```
runtime error: invalid memory address or nil pointer dereference
github.com/prometheus/client_golang/prometheus.(*GaugeVec).GetMetricWithLabelValues
github.com/weaveworks/common/middleware.(*Instrument).Wrap
```

## Root Cause
The `weaveworks/common/middleware.Instrument` struct requires several Prometheus metrics to be initialized:
- `Duration` (HistogramVec) ✅ Already had
- `InflightRequests` (GaugeVec) ❌ Missing
- `RequestBodySize` (HistogramVec) ❌ Missing  
- `ResponseBodySize` (HistogramVec) ❌ Missing

When these metrics are `nil`, the middleware panics when trying to record metrics.

## Solution
Added all required Prometheus metrics in `wiring.go`:

### Added Metrics:
1. **HTTPInflightRequests** - Tracks current number of inflight requests
2. **HTTPRequestBodySize** - Tracks HTTP request body sizes
3. **HTTPResponseBodySize** - Tracks HTTP response body sizes

### Code Changes:
```go
var (
    HTTPLatency = prometheus.NewHistogramVec(...)  // Already existed
    
    // Added:
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
```

### Updated Middleware Configuration:
```go
httpMiddleware := []middleware.Interface{
    middleware.Instrument{
        Duration:         HTTPLatency,
        RouteMatcher:     router,
        InflightRequests: HTTPInflightRequests,      // Added
        RequestBodySize:  HTTPRequestBodySize,       // Added
        ResponseBodySize: HTTPResponseBodySize,      // Added
    },
}
```

## Testing Results

### Local Testing
```bash
✅ go build successful
✅ ./paymentsvc -port=8080 runs without panics
✅ Health endpoint works: /health
✅ Payment auth works: /paymentAuth
✅ Metrics endpoint works: /metrics
```

### Docker Testing  
```bash
✅ Docker image builds successfully
✅ Container runs without crashes
✅ All endpoints respond correctly
✅ No panic errors in logs
```

### Test Output:
```json
# Health check
{"health":[{"service":"payment","status":"OK","time":"..."}]}

# Payment authorization
{"authorised":true,"message":"Payment authorised"}
```

### Logs (No Errors):
```
ts=2026-01-06T10:42:36.669765524Z caller=main.go:80 transport=HTTP port=80
ts=2026-01-06T10:42:39.761858605Z caller=logging.go:36 method=Health result=1 took=68.896µs
ts=2026-01-06T10:42:39.788380472Z caller=logging.go:25 method=Authorise result=true took=389ns
```

## Deployment
The fixed version is available as:
- `weaveworksdemos/payment:v1.0-fixed`
- `weaveworksdemos/payment:latest`

Deploy to Kubernetes:
```bash
kubectl set image deployment/payment payment=weaveworksdemos/payment:v1.0-fixed -n sock-shop
```

## Summary
The issue was caused by incomplete Prometheus metrics initialization after upgrading to modern go-kit and weaveworks/common middleware. All required metrics are now properly initialized and registered, fixing the nil pointer dereference crashes.
