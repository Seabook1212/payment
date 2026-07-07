package payment

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/go-kit/kit/log"
	kitopentracing "github.com/go-kit/kit/tracing/opentracing"
	opentracing "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/mocktracer"
)

func findFinishedSpanByOperation(spans []*mocktracer.MockSpan, operationName string) *mocktracer.MockSpan {
	for _, span := range spans {
		if span.OperationName == operationName {
			return span
		}
	}
	return nil
}

func TestPaymentSpansJoinUpstreamTrace(t *testing.T) {
	tracer := mocktracer.New()
	upstreamSpan := tracer.StartSpan("upstream")
	defer upstreamSpan.Finish()

	req := httptest.NewRequest("POST", "http://payment/paymentAuth", nil)
	if err := tracer.Inject(upstreamSpan.Context(), opentracing.HTTPHeaders, opentracing.HTTPHeadersCarrier(req.Header)); err != nil {
		t.Fatalf("inject upstream context failed: %v", err)
	}

	upstreamCtx := upstreamSpan.Context().(mocktracer.MockSpanContext)
	ctx := kitopentracing.HTTPToContext(tracer, "POST /paymentAuth", log.NewNopLogger())(context.Background(), req)

	tracer.Reset()

	endpoint := serverTraceEndpointMiddleware(tracer, "POST /paymentAuth")(
		MakeAuthoriseEndpoint(NewAuthorisationService(100000000), tracer),
	)

	response, err := endpoint(ctx, AuthoriseRequest{Amount: 10})
	if err != nil {
		t.Fatalf("endpoint returned unexpected error: %v", err)
	}
	if _, ok := response.(AuthoriseResponse); !ok {
		t.Fatalf("unexpected response type: %T", response)
	}

	spans := tracer.FinishedSpans()
	serverSpan := findFinishedSpanByOperation(spans, "POST /paymentAuth")
	internalSpan := findFinishedSpanByOperation(spans, "authorize payment")

	if serverSpan == nil {
		t.Fatalf("server span not found in finished spans: %#v", spans)
	}
	if internalSpan == nil {
		t.Fatalf("internal span not found in finished spans: %#v", spans)
	}
	if serverSpan.SpanContext.TraceID != upstreamCtx.TraceID {
		t.Fatalf("server span trace id mismatch: got %d want %d", serverSpan.SpanContext.TraceID, upstreamCtx.TraceID)
	}
	if internalSpan.SpanContext.TraceID != serverSpan.SpanContext.TraceID {
		t.Fatalf("internal span trace id mismatch: got %d want %d", internalSpan.SpanContext.TraceID, serverSpan.SpanContext.TraceID)
	}
	if internalSpan.ParentID != serverSpan.SpanContext.SpanID {
		t.Fatalf("internal span parent mismatch: got %d want %d", internalSpan.ParentID, serverSpan.SpanContext.SpanID)
	}
}
