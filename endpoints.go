package payment

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/kit/endpoint"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/tracing/opentracing"
	stdopentracing "github.com/opentracing/opentracing-go"
	zipkintracer "github.com/openzipkin-contrib/zipkin-go-opentracing"
)

// Endpoints collects the endpoints that comprise the Service.
type Endpoints struct {
	AuthoriseEndpoint endpoint.Endpoint
	HealthEndpoint    endpoint.Endpoint
}

// extractTraceIDs extracts trace ID and span ID from context for logging
func extractTraceIDs(ctx context.Context) (traceID, spanID string) {
	span := stdopentracing.SpanFromContext(ctx)
	if span != nil {
		sc := span.Context()

		// Try to cast to Zipkin SpanContext type
		if zipkinSC, ok := sc.(zipkintracer.SpanContext); ok {
			// Extract the trace ID and span ID from Zipkin span context
			// SpanContext has TraceID and ID fields directly
			traceID = zipkinSC.TraceID.String()
			spanID = zipkinSC.ID.String()
			return
		}

		// Fallback: try string representation
		str := fmt.Sprintf("%v", sc)
		if len(str) > 0 {
			traceID = str
			spanID = str
		}
	}
	return
}

// loggingEndpointMiddleware logs endpoint calls with trace context
func loggingEndpointMiddleware(logger log.Logger) endpoint.Middleware {
	return func(next endpoint.Endpoint) endpoint.Endpoint {
		return func(ctx context.Context, request interface{}) (response interface{}, err error) {
			defer func(begin time.Time) {
				traceID, spanID := extractTraceIDs(ctx)

				// Extract method name and result
				method := "Unknown"
				result := ""

				if resp, ok := response.(AuthoriseResponse); ok {
					method = "Authorise"
					if resp.Err != nil {
						err = resp.Err
					}
					result = fmt.Sprintf("%v", resp.Authorisation.Authorised)

					// Log with trace context (matching catalogue service format)
					logger.Log(
						"traceid", traceID,
						"spanid", spanID,
						"method", method,
						"result", result,
						"err", err,
						"took", time.Since(begin),
					)
				}
			}(time.Now())

			return next(ctx, request)
		}
	}
}

// MakeEndpoints returns an Endpoints structure, where each endpoint is
// backed by the given service.
func MakeEndpoints(s Service, tracer stdopentracing.Tracer, logger log.Logger) Endpoints {
	authoriseEndpoint := MakeAuthoriseEndpoint(s)
	authoriseEndpoint = loggingEndpointMiddleware(logger)(authoriseEndpoint)
	authoriseEndpoint = opentracing.TraceServer(tracer, "POST /paymentAuth")(authoriseEndpoint)

	return Endpoints{
		AuthoriseEndpoint: authoriseEndpoint,
		HealthEndpoint:    MakeHealthEndpoint(s), // No tracing for health checks
	}
}

// MakeListEndpoint returns an endpoint via the given service.
func MakeAuthoriseEndpoint(s Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (response interface{}, err error) {
		var span stdopentracing.Span
		span, ctx = stdopentracing.StartSpanFromContext(ctx, "authorize payment")
		span.SetTag("service", "payment")
		defer span.Finish()
		req := request.(AuthoriseRequest)
		authorisation, err := s.Authorise(req.Amount)
		return AuthoriseResponse{Authorisation: authorisation, Err: err}, nil
	}
}

// MakeHealthEndpoint returns current health of the given service.
// Health checks are not traced to reduce noise in tracing systems.
func MakeHealthEndpoint(s Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (response interface{}, err error) {
		health := s.Health()
		return healthResponse{Health: health}, nil
	}
}

// AuthoriseRequest represents a request for payment authorisation.
// The Amount is the total amount of the transaction
type AuthoriseRequest struct {
	Amount float32 `json:"amount"`
}

// AuthoriseResponse returns a response of type Authorisation and an error, Err.
type AuthoriseResponse struct {
	Authorisation Authorisation
	Err           error
}

type healthRequest struct {
	//
}

type healthResponse struct {
	Health []Health `json:"health"`
}
