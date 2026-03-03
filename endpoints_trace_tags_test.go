package payment

import (
	"reflect"
	"testing"
)

func TestTraceTagsFromEnv(t *testing.T) {
	t.Setenv("CONTAINER_NAME", "payment")
	t.Setenv("POD_NAME", "payment-6d95dcf9b4-abcde")
	t.Setenv("POD_NAMESPACE", "sock-shop")
	t.Setenv("NODE_NAME", "instance-20250929-075527")

	got := traceTagsFromEnv()
	want := map[string]string{
		"container": "payment",
		"pod":       "payment-6d95dcf9b4-abcde",
		"namespace": "sock-shop",
		"node":      "instance-20250929-075527",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("traceTagsFromEnv mismatch: got %v want %v", got, want)
	}
}

func TestTraceTagsFromEnvSkipsEmptyValues(t *testing.T) {
	t.Setenv("CONTAINER_NAME", "")
	t.Setenv("POD_NAME", "payment-6d95dcf9b4-abcde")
	t.Setenv("POD_NAMESPACE", "")
	t.Setenv("NODE_NAME", "instance-20250929-075527")

	got := traceTagsFromEnv()
	want := map[string]string{
		"pod":  "payment-6d95dcf9b4-abcde",
		"node": "instance-20250929-075527",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("traceTagsFromEnv mismatch: got %v want %v", got, want)
	}
}
