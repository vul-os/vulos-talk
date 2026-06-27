package bots

import "testing"

func TestValidateEventURL_AllowsPublicHTTPS(t *testing.T) {
	// A literal public IP avoids DNS in tests while still exercising the IP guard.
	for _, u := range []string{
		"https://93.184.216.34/hook", // example.com's documented address range
		"http://8.8.8.8:8080/events",
		"", // optional → allowed
	} {
		if err := ValidateEventURL(u); err != nil {
			t.Errorf("ValidateEventURL(%q) = %v; want nil", u, err)
		}
	}
}

func TestValidateEventURL_RejectsPrivateAndMetadata(t *testing.T) {
	for _, u := range []string{
		"http://127.0.0.1/x",            // loopback
		"http://10.0.0.5/x",             // RFC1918
		"http://192.168.1.1/x",          // RFC1918
		"http://172.16.0.1/x",           // RFC1918
		"http://169.254.169.254/latest", // cloud metadata / link-local
		"http://[::1]/x",                // IPv6 loopback
		"http://0.0.0.0/x",              // unspecified
	} {
		if err := ValidateEventURL(u); err == nil {
			t.Errorf("ValidateEventURL(%q) = nil; want rejection", u)
		}
	}
}

func TestValidateEventURL_RejectsBadScheme(t *testing.T) {
	for _, u := range []string{
		"ftp://93.184.216.34/x",
		"file:///etc/passwd",
		"gopher://93.184.216.34/x",
	} {
		if err := ValidateEventURL(u); err == nil {
			t.Errorf("ValidateEventURL(%q) = nil; want scheme rejection", u)
		}
	}
}

func TestValidateEventURL_SelfHostOptOut(t *testing.T) {
	t.Setenv(EnvAllowPrivateWebhooks, "1")
	if err := ValidateEventURL("http://127.0.0.1/x"); err != nil {
		t.Fatalf("opt-out should allow private target, got %v", err)
	}
}
