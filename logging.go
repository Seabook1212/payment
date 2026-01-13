package payment

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/opentracing/opentracing-go"
)

// extractTraceInfo extracts traceid and spanid from context
func extractTraceInfo(ctx context.Context) (traceID, spanID string) {
	span := opentracing.SpanFromContext(ctx)
	if span != nil {
		spanCtx := span.Context()
		// Convert to string - format depends on tracer implementation
		traceID = fmt.Sprintf("%v", spanCtx)
		// Try to extract IDs from span context
		if sc, ok := spanCtx.(interface {
			TraceID() string
			SpanID() string
		}); ok {
			traceID = sc.TraceID()
			spanID = sc.SpanID()
		} else {
			// Fallback: parse from string representation
			traceID = fmt.Sprintf("%v", spanCtx)
			spanID = traceID
		}
	}
	return
}

// LoggingMiddleware logs method calls, parameters, results, and elapsed time.
func LoggingMiddleware(logger log.Logger) Middleware {
	return func(next Service) Service {
		return loggingMiddleware{
			next:   next,
			logger: logger,
		}
	}
}

type loggingMiddleware struct {
	next   Service
	logger log.Logger
}

func (mw loggingMiddleware) Authorise(amount float32) (auth Authorisation, err error) {
	defer func(begin time.Time) {
		mw.logger.Log(
			"method", "Authorise",
			"amount", amount,
			"result", auth.Authorised,
			"err", err,
			"took", time.Since(begin),
		)
	}(time.Now())
	return mw.next.Authorise(amount)
}

func (mw loggingMiddleware) Health() (health []Health) {
	// Health endpoint intentionally not logged to reduce noise
	return mw.next.Health()
}
