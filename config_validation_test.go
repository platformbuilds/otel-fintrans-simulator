package main

import (
	"strings"
	"testing"
)

func TestValidateTelemetryConfig_HTTPInsecureMismatch(t *testing.T) {
	// http scheme but insecure=false should warn
	warnings := validateTelemetryConfig("http://localhost:4318", false, false)
	if len(warnings) == 0 {
		t.Fatalf("expected warning for http scheme with insecure=false, got none")
	}
}

func TestValidateTelemetryConfig_HTTPSInsecureMismatch(t *testing.T) {
	// https scheme but insecure=true should warn
	warnings := validateTelemetryConfig("https://localhost:4318", true, false)
	if len(warnings) == 0 {
		t.Fatalf("expected warning for https scheme with insecure=true, got none")
	}
}

func TestValidateTelemetryConfig_SkipVerifyIgnored(t *testing.T) {
	// skipVerify true but insecure true -> warning
	warnings := validateTelemetryConfig("localhost:4317", true, true)
	if len(warnings) == 0 {
		t.Fatalf("expected warning when skipVerify set together with insecure=true, got none")
	}
}

func TestValidateTelemetryConfig_HTTPOn4317(t *testing.T) {
	// http scheme on port 4317 should emit a specific warning
	warnings := validateTelemetryConfig("http://localhost:4317", false, false)
	if len(warnings) == 0 {
		t.Fatalf("expected warning for http:// on port 4317, got none")
	}
	// ensure the message mentions 4317 so it's helpful
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "4317") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected a warning mentioning '4317', got: %v", warnings)
	}
}

func TestValidateTelemetryConfig_Localhost4317_NoScheme(t *testing.T) {
	// no scheme and using :4317 should be fine (gRPC canonical style)
	warnings := validateTelemetryConfig("localhost:4317", false, false)
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings for localhost:4317, got: %v", warnings)
	}
}
