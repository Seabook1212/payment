package payment

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-kit/kit/log"
	"github.com/opentracing/opentracing-go/mocktracer"
)

func TestHTTPServerSpanIncludesMethodStatusAndURLTags(t *testing.T) {
	tracer := mocktracer.New()
	logger := log.NewNopLogger()
	service := NewAuthorisationService(100000000)
	endpoints := MakeEndpoints(service, tracer, logger)
	handler := MakeHTTPHandler(context.Background(), endpoints, logger, tracer)

	req := httptest.NewRequest(
		http.MethodPost,
		"http://payment/paymentAuth?page=1&size=6&tags=blue",
		bytes.NewBufferString(`{"amount":10}`),
	)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status code: got %d want %d", recorder.Code, http.StatusOK)
	}

	serverSpan := findFinishedSpanByOperation(tracer.FinishedSpans(), "POST /paymentAuth")
	if serverSpan == nil {
		t.Fatalf("server span not found in finished spans: %#v", tracer.FinishedSpans())
	}
	if got, ok := serverSpan.Tag("http.method").(string); !ok || got != http.MethodPost {
		t.Fatalf("unexpected http.method tag: got %#v want %q", serverSpan.Tag("http.method"), http.MethodPost)
	}
	if got, ok := serverSpan.Tag("http.status_code").(int); !ok || got != http.StatusOK {
		t.Fatalf("unexpected http.status_code tag: got %#v want %d", serverSpan.Tag("http.status_code"), http.StatusOK)
	}
	if got, ok := serverSpan.Tag("http.url").(string); !ok || got != "/paymentAuth?page=1&size=6&tags=blue" {
		t.Fatalf("unexpected http.url tag: got %#v want %q", serverSpan.Tag("http.url"), "/paymentAuth?page=1&size=6&tags=blue")
	}
}
