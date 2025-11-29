package main

import "testing"

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
