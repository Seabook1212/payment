package main

import "testing"

func TestResolveZipkinAddressFromValues(t *testing.T) {
	tests := []struct {
		name         string
		zipkinAddr   string
		zipkinBase   string
		zipkinHost   string
		zipkinPort   string
		expectedAddr string
	}{
		{
			name:         "zipkin env takes highest priority",
			zipkinAddr:   "http://custom-zipkin:9411",
			zipkinBase:   "http://base-url:9411",
			zipkinHost:   "host-a",
			zipkinPort:   "1234",
			expectedAddr: "http://custom-zipkin:9411",
		},
		{
			name:         "base url used when zipkin env is missing",
			zipkinAddr:   "",
			zipkinBase:   "http://from-base-url:9411",
			zipkinHost:   "host-a",
			zipkinPort:   "1234",
			expectedAddr: "http://from-base-url:9411",
		},
		{
			name:         "host and port used when base url is missing",
			zipkinAddr:   "",
			zipkinBase:   "",
			zipkinHost:   "jaeger-collector.observability.svc.cluster.local",
			zipkinPort:   "9411",
			expectedAddr: "http://jaeger-collector.observability.svc.cluster.local:9411",
		},
		{
			name:         "hardcoded defaults used when all env vars are missing",
			zipkinAddr:   "",
			zipkinBase:   "",
			zipkinHost:   "",
			zipkinPort:   "",
			expectedAddr: "http://jaeger-collector.observability.svc.cluster.local:9411",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveZipkinAddressFromValues(tc.zipkinAddr, tc.zipkinBase, tc.zipkinHost, tc.zipkinPort)
			if got != tc.expectedAddr {
				t.Fatalf("resolveZipkinAddressFromValues() mismatch: got %q want %q", got, tc.expectedAddr)
			}
		})
	}
}
