package payment

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-kit/kit/endpoint"
	"github.com/go-kit/kit/log"
	httptransport "github.com/go-kit/kit/transport/http"
	stdopentracing "github.com/opentracing/opentracing-go"
	zipkintracer "github.com/openzipkin-contrib/zipkin-go-opentracing"
)

// Endpoints collects the endpoints that comprise the Service.
type Endpoints struct {
	AuthoriseEndpoint endpoint.Endpoint
	HealthEndpoint    endpoint.Endpoint
}

const (
	spanKindTag      = "span.kind"
	spanKindServer   = "server"
	spanKindInternal = "internal"
	httpMethodTag    = "http.method"
	httpURLTag       = "http.url"
)

var traceTagEnvMappings = []struct {
	tagKey string
	envKey string
}{
	{tagKey: "container", envKey: "CONTAINER_NAME"},
	{tagKey: "pod", envKey: "POD_NAME"},
	{tagKey: "namespace", envKey: "POD_NAMESPACE"},
	{tagKey: "node", envKey: "NODE_NAME"},
}

func traceTagsFromEnv() map[string]string {
	tags := make(map[string]string, len(traceTagEnvMappings))
	for _, mapping := range traceTagEnvMappings {
		if value, ok := os.LookupEnv(mapping.envKey); ok && value != "" {
			tags[mapping.tagKey] = value
		}
	}
	return tags
}

func applyEnvironmentTraceTags(span stdopentracing.Span) {
	for key, value := range traceTagsFromEnv() {
		span.SetTag(key, value)
	}
}

func applyHTTPTraceTagsFromContext(ctx context.Context, span stdopentracing.Span) {
	if method, ok := ctx.Value(httptransport.ContextKeyRequestMethod).(string); ok && method != "" {
		span.SetTag(httpMethodTag, method)
	}
	if requestURI, ok := ctx.Value(httptransport.ContextKeyRequestURI).(string); ok && requestURI != "" {
		span.SetTag(httpURLTag, normalizeHTTPURLTagValue(requestURI))
	}
}

func tagSpanError(span stdopentracing.Span, err error) {
	if span == nil || err == nil {
		return
	}

	errorType := classifyAuthoriseError(err)
	if errorType == "" {
		errorType = "internal"
	}

	span.SetTag("error", true)
	span.SetTag("error.type", errorType)
	span.SetTag("error.message", err.Error())
	span.SetTag("exception.type", errorType)
	span.SetTag("exception.message", err.Error())
}

func tagSpanException(span stdopentracing.Span, exceptionType, message string) {
	if span == nil {
		return
	}

	if exceptionType == "" {
		exceptionType = "internal"
	}

	span.SetTag("error", true)
	span.SetTag("error.type", exceptionType)
	span.SetTag("error.message", message)
	span.SetTag("exception.type", exceptionType)
	span.SetTag("exception.message", message)
}

func normalizeHTTPURLTagValue(requestURI string) string {
	if strings.HasPrefix(requestURI, "http://") || strings.HasPrefix(requestURI, "https://") {
		if parsed, err := url.Parse(requestURI); err == nil {
			if parsed.RawQuery == "" {
				return parsed.Path
			}
			return parsed.Path + "?" + parsed.RawQuery
		}
	}
	return requestURI
}

func hasHTTPRequestContext(ctx context.Context) bool {
	method, ok := ctx.Value(httptransport.ContextKeyRequestMethod).(string)
	return ok && method != ""
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
				resp, ok := response.(AuthoriseResponse)
				if !ok {
					return
				}

				traceID, spanID := extractTraceIDs(ctx)
				result := fmt.Sprintf("%v", resp.Authorisation.Authorised)
				level := "info"
				if resp.Err != nil {
					level = "error"
				}
				_ = logger.Log(
					"level", level,
					"traceid", traceID,
					"spanid", spanID,
					"method", "Authorise",
					"result", result,
					"err", resp.Err,
					"operation", "authorise_payment",
					"target", "/paymentAuth",
					"error_type", classifyAuthoriseError(resp.Err),
					"error", resp.Err,
					"took", time.Since(begin),
				)
			}(time.Now())

			return next(ctx, request)
		}
	}
}

// serverTraceEndpointMiddleware ensures inbound spans are server spans.
// If HTTPToContext already placed a span in ctx, we reuse and finish it.
func serverTraceEndpointMiddleware(tracer stdopentracing.Tracer, operationName string) endpoint.Middleware {
	return func(next endpoint.Endpoint) endpoint.Endpoint {
		return func(ctx context.Context, request interface{}) (response interface{}, err error) {
			serverSpan := stdopentracing.SpanFromContext(ctx)
			hasInboundSpan := serverSpan != nil
			if serverSpan == nil {
				serverSpan = tracer.StartSpan(operationName, stdopentracing.Tag{
					Key:   spanKindTag,
					Value: spanKindServer,
				})
				ctx = stdopentracing.ContextWithSpan(ctx, serverSpan)
			} else {
				serverSpan.SetOperationName(operationName)
			}
			applyEnvironmentTraceTags(serverSpan)
			applyHTTPTraceTagsFromContext(ctx, serverSpan)
			// For HTTP requests we finish spans in the transport finalizer after status is known.
			if !hasInboundSpan || !hasHTTPRequestContext(ctx) {
				defer serverSpan.Finish()
			}
			return next(ctx, request)
		}
	}
}

// MakeEndpoints returns an Endpoints structure, where each endpoint is
// backed by the given service.
func MakeEndpoints(s Service, tracer stdopentracing.Tracer, logger log.Logger) Endpoints {
	authoriseEndpoint := MakeAuthoriseEndpoint(s, tracer)
	authoriseEndpoint = loggingEndpointMiddleware(logger)(authoriseEndpoint)
	authoriseEndpoint = serverTraceEndpointMiddleware(tracer, "POST /paymentAuth")(authoriseEndpoint)

	return Endpoints{
		AuthoriseEndpoint: authoriseEndpoint,
		HealthEndpoint:    MakeHealthEndpoint(s), // No tracing for health checks
	}
}

// MakeListEndpoint returns an endpoint via the given service.
func MakeAuthoriseEndpoint(s Service, tracer stdopentracing.Tracer) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (response interface{}, err error) {
		var span stdopentracing.Span
		span, ctx = stdopentracing.StartSpanFromContextWithTracer(ctx, tracer, "authorize payment", stdopentracing.Tag{
			Key:   spanKindTag,
			Value: spanKindInternal,
		})
		span.SetTag("service", "payment")
		applyEnvironmentTraceTags(span)
		defer span.Finish()
		req := request.(AuthoriseRequest)
		authorisation, err := s.Authorise(req.Amount)
		tagSpanError(span, err)
		return AuthoriseResponse{Authorisation: authorisation, Err: err}, nil
	}
}

func classifyAuthoriseError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrInvalidPaymentAmount):
		return "validation"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return "internal"
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
