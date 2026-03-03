package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-kit/kit/circuitbreaker"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/tracing/opentracing"
	kittransport "github.com/go-kit/kit/transport"
	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/gorilla/mux"
	stdopentracing "github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/streadway/handy/breaker"
)

// MakeHTTPHandler mounts the endpoints into a REST-y HTTP handler.
func MakeHTTPHandler(ctx context.Context, e Endpoints, logger log.Logger, tracer stdopentracing.Tracer) *mux.Router {
	r := mux.NewRouter().StrictSlash(false)
	r.Use(httpBoundaryMiddleware(logger))

	options := []httptransport.ServerOption{
		httptransport.ServerErrorHandler(kittransport.ErrorHandlerFunc(func(ctx context.Context, err error) {
			logHTTPBoundaryError(logger, ctx, err)
		})),
		httptransport.ServerErrorEncoder(encodeError),
		httptransport.ServerFinalizer(func(ctx context.Context, code int, _ *http.Request) {
			span := stdopentracing.SpanFromContext(ctx)
			if span == nil {
				return
			}
			span.SetTag("http.status_code", code)
			span.Finish()
		}),
	}

	r.Methods("POST").Path("/paymentAuth").Handler(httptransport.NewServer(
		circuitbreaker.HandyBreaker(breaker.NewBreaker(0.2))(e.AuthoriseEndpoint),
		decodeAuthoriseRequest,
		encodeAuthoriseResponse,
		append(options, httptransport.ServerBefore(
			httptransport.PopulateRequestContext,
			opentracing.HTTPToContext(tracer, "POST /paymentAuth", logger),
		))...,
	))
	r.Methods("GET").Path("/health").Handler(httptransport.NewServer(
		circuitbreaker.HandyBreaker(breaker.NewBreaker(0.2))(e.HealthEndpoint),
		decodeHealthRequest,
		encodeHealthResponse,
		options...,
	))
	r.Handle("/metrics", promhttp.Handler())
	return r
}

func encodeError(_ context.Context, err error, w http.ResponseWriter) {
	code := http.StatusInternalServerError
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":       err.Error(),
		"status_code": code,
		"status_text": http.StatusText(code),
	})
}

func decodeAuthoriseRequest(_ context.Context, r *http.Request) (interface{}, error) {
	// Read the content
	var bodyBytes []byte
	if r.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			return nil, fmt.Errorf("read authorise request body: %w", err)
		}
	}
	// Save the content
	bodyString := string(bodyBytes)

	// Decode auth request
	var request AuthoriseRequest
	if err := json.Unmarshal(bodyBytes, &request); err != nil {
		return nil, fmt.Errorf("decode authorise request body: %w", err)
	}

	// If amount isn't present, error
	if request.Amount == 0.0 {
		return nil, &UnmarshalKeyError{
			Key:  "amount",
			JSON: bodyString,
		}
	}
	return request, nil
}

type UnmarshalKeyError struct {
	Key  string
	JSON string
}

func (e *UnmarshalKeyError) Error() string {
	return fmt.Sprintf("Cannot unmarshal object key %q from JSON: %s", e.Key, e.JSON)
}

var ErrInvalidJson = errors.New("Invalid json")

func encodeAuthoriseResponse(ctx context.Context, w http.ResponseWriter, response interface{}) error {
	resp := response.(AuthoriseResponse)
	if resp.Err != nil {
		encodeError(ctx, resp.Err, w)
		return nil
	}
	return encodeResponse(ctx, w, resp.Authorisation)
}

func decodeHealthRequest(_ context.Context, r *http.Request) (interface{}, error) {
	return struct{}{}, nil
}

func encodeHealthResponse(ctx context.Context, w http.ResponseWriter, response interface{}) error {
	return encodeResponse(ctx, w, response.(healthResponse))
}

func encodeResponse(_ context.Context, w http.ResponseWriter, response interface{}) error {
	// All of our response objects are JSON serializable, so we just do that.
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	return json.NewEncoder(w).Encode(response)
}

type requestStartKey struct{}

func httpBoundaryMiddleware(logger log.Logger) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), requestStartKey{}, time.Now())
			r = r.WithContext(ctx)

			defer func() {
				if recovered := recover(); recovered != nil {
					logHTTPPanic(logger, r.Context(), r, recovered)
					encodeError(r.Context(), fmt.Errorf("panic: %v", recovered), w)
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

func logHTTPBoundaryError(logger log.Logger, ctx context.Context, err error) {
	if err == nil {
		return
	}

	tagSpanError(stdopentracing.SpanFromContext(ctx), err)

	traceID, spanID := extractTraceIDs(ctx)
	operation, target := requestOperationFromContext(ctx)
	latencyMS := requestLatencyMilliseconds(ctx)

	_ = logger.Log(
		"level", "error",
		"traceid", traceID,
		"spanid", spanID,
		"operation", operation,
		"dependency", "client",
		"target", target,
		"error_type", classifyTransportError(err),
		"error", safeErrorMessage(err),
		"latency_ms", latencyMS,
	)
}

func logHTTPPanic(logger log.Logger, ctx context.Context, r *http.Request, recovered interface{}) {
	panicMessage := fmt.Sprintf("%v", recovered)
	panicType := fmt.Sprintf("%T", recovered)
	tagSpanException(stdopentracing.SpanFromContext(ctx), panicType, panicMessage)

	traceID, spanID := extractTraceIDs(ctx)
	latencyMS := requestLatencyMilliseconds(ctx)

	_ = logger.Log(
		"level", "error",
		"traceid", traceID,
		"spanid", spanID,
		"operation", r.Method+" "+r.URL.Path,
		"dependency", "client",
		"target", r.URL.Path,
		"error_type", "panic",
		"error", panicMessage,
		"latency_ms", latencyMS,
		"stack", string(debug.Stack()),
	)
}

func requestOperationFromContext(ctx context.Context) (operation string, target string) {
	method, _ := ctx.Value(httptransport.ContextKeyRequestMethod).(string)
	target, _ = ctx.Value(httptransport.ContextKeyRequestPath).(string)
	if target == "" {
		target, _ = ctx.Value(httptransport.ContextKeyRequestURI).(string)
	}
	if method == "" {
		method = "UNKNOWN"
	}
	if target == "" {
		target = "unknown"
	}
	return method + " " + target, target
}

func requestLatencyMilliseconds(ctx context.Context) int64 {
	start, ok := ctx.Value(requestStartKey{}).(time.Time)
	if !ok || start.IsZero() {
		return 0
	}
	return time.Since(start).Milliseconds()
}

func classifyTransportError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, breaker.ErrCircuitOpen):
		return "circuit_open"
	case errors.Is(err, ErrInvalidPaymentAmount):
		return "validation"
	}

	var missingKeyErr *UnmarshalKeyError
	if errors.As(err, &missingKeyErr) {
		return "validation"
	}

	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return "validation"
	}

	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return "validation"
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}

	return "internal"
}

func safeErrorMessage(err error) string {
	var missingKeyErr *UnmarshalKeyError
	if errors.As(err, &missingKeyErr) {
		return fmt.Sprintf("missing required field %q", missingKeyErr.Key)
	}
	return err.Error()
}
