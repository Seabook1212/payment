package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/microservices-demo/payment"
	stdopentracing "github.com/opentracing/opentracing-go"
	zipkintracer "github.com/openzipkin-contrib/zipkin-go-opentracing"
	"github.com/openzipkin/zipkin-go"
	zipkinhttp "github.com/openzipkin/zipkin-go/reporter/http"
)

const (
	ServiceName       = "payment"
	defaultZipkinHost = "jaeger-collector.observability.svc.cluster.local"
	defaultZipkinPort = "9411"
)

func resolveZipkinAddressFromValues(zipkinAddr, zipkinBaseURL, zipkinHost, zipkinPort string) string {
	zipkinAddr = strings.TrimSpace(zipkinAddr)
	if zipkinAddr != "" {
		return zipkinAddr
	}

	zipkinBaseURL = strings.TrimSpace(zipkinBaseURL)
	if zipkinBaseURL != "" {
		return zipkinBaseURL
	}

	zipkinHost = strings.TrimSpace(zipkinHost)
	if zipkinHost == "" {
		zipkinHost = defaultZipkinHost
	}

	zipkinPort = strings.TrimSpace(zipkinPort)
	if zipkinPort == "" {
		zipkinPort = defaultZipkinPort
	}

	return fmt.Sprintf("http://%s:%s", zipkinHost, zipkinPort)
}

func resolveZipkinAddressFromEnv() string {
	return resolveZipkinAddressFromValues(
		os.Getenv("ZIPKIN"),
		os.Getenv("ZIPKIN_BASE_URL"),
		os.Getenv("ZIPKIN_HOST"),
		os.Getenv("ZIPKIN_PORT"),
	)
}

func main() {
	var (
		port          = flag.String("port", "80", "Port to bind HTTP listener")
		zip           = flag.String("zipkin", resolveZipkinAddressFromEnv(), "Zipkin address")
		declineAmount = flag.Float64("decline", 100000000, "Decline payments over certain amount")
	)
	flag.Parse()

	var logger log.Logger
	{
		logger = log.NewLogfmtLogger(os.Stderr)
		logger = log.With(logger, "ts", log.DefaultTimestampUTC)
		logger = log.With(logger, "caller", log.DefaultCaller)
		logger = log.With(logger, "service", ServiceName)
	}

	var tracer stdopentracing.Tracer
	{
		// Find service local IP.
		dialCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		conn, err := (&net.Dialer{}).DialContext(dialCtx, "udp", "8.8.8.8:80")
		if err != nil {
			_ = logger.Log(
				"level", "error",
				"operation", "startup.resolve_local_address",
				"dependency", "network",
				"target", "8.8.8.8:80",
				"error_type", classifyRuntimeError(err),
				"error", fmt.Errorf("probe local address: %w", err),
			)
			os.Exit(1)
		}
		localAddr := conn.LocalAddr().(*net.UDPAddr)
		host := strings.Split(localAddr.String(), ":")[0]
		defer conn.Close()
		if *zip == "" {
			tracer = stdopentracing.NoopTracer{}
		} else {
			reporterLogger := log.With(logger, "dependency", "zipkin", "target", *zip)
			_ = reporterLogger.Log(
				"level", "info",
				"operation", "startup.init_tracer",
				"tracer", "zipkin",
				"addr", *zip,
			)

			reporter := zipkinhttp.NewReporter(*zip)
			defer reporter.Close()

			endpoint, err := zipkin.NewEndpoint(ServiceName, fmt.Sprintf("%v:%v", host, *port))
			if err != nil {
				_ = reporterLogger.Log(
					"level", "error",
					"operation", "startup.init_tracer",
					"error_type", "configuration",
					"error", fmt.Errorf("create zipkin endpoint: %w", err),
				)
				os.Exit(1)
			}
			nativeTracer, err := zipkin.NewTracer(
				reporter,
				zipkin.WithLocalEndpoint(endpoint),
				zipkin.WithSharedSpans(false),
			)
			if err != nil {
				_ = reporterLogger.Log(
					"level", "error",
					"operation", "startup.init_tracer",
					"error_type", classifyRuntimeError(err),
					"error", fmt.Errorf("create zipkin tracer: %w", err),
				)
				os.Exit(1)
			}
			tracer = zipkintracer.Wrap(nativeTracer)
		}
		stdopentracing.InitGlobalTracer(tracer)

	}
	// Mechanical stuff.
	errc := make(chan error)
	ctx := context.Background()

	handler, logger := payment.WireUp(ctx, float32(*declineAmount), tracer, ServiceName)

	// Create and launch the HTTP server.
	go func() {
		_ = logger.Log("level", "info", "transport", "HTTP", "port", *port)
		errc <- fmt.Errorf("listen on %s: %w", *port, http.ListenAndServe(":"+*port, handler))
	}()

	// Capture interrupts.
	go func() {
		c := make(chan os.Signal)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		errc <- fmt.Errorf("%s", <-c)
	}()

	err := <-errc
	if isShutdownSignal(err) {
		_ = logger.Log("level", "info", "operation", "shutdown", "signal", err.Error())
		return
	}

	_ = logger.Log(
		"level", "error",
		"operation", "http_server",
		"dependency", "client",
		"target", ":"+*port,
		"error_type", classifyRuntimeError(err),
		"error", err,
	)
}

func classifyRuntimeError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return "timeout"
		}
		return "connection"
	}

	if strings.Contains(err.Error(), "bind:") {
		return "listen"
	}

	return "internal"
}

func isShutdownSignal(err error) bool {
	if err == nil {
		return false
	}
	switch err.Error() {
	case syscall.SIGINT.String(), syscall.SIGTERM.String():
		return true
	default:
		return false
	}
}
