package httpegress_test

import (
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/httpegress"
)

func TestValidateLabCatcherAndLocal(t *testing.T) {
	t.Parallel()
	ok := []string{
		"http://127.0.0.1:4588" + httpegress.LabHTTPCatcherPath,
		"http://127.0.0.1:4588" + httpegress.LabHTTPCatcherPath + "/x",
		"http://localhost:4588/v2/projects/p/locations/us/services/s:invoke",
		"http://[::1]:4588/_noctaxris-gcp/health",
		"http://127.0.0.1:59999/_noctaxris-gcp/oidc-lab/.well-known/jwks.json",
	}
	for _, ep := range ok {
		if err := httpegress.Validate(ep); err != nil {
			t.Fatalf("%s: %v", ep, err)
		}
	}
}

func TestValidateRejectsPrivateWithoutEgress(t *testing.T) {
	t.Setenv(httpegress.EnvHTTPEgress, "")
	t.Setenv(httpegress.EnvHTTPAllowlist, "")
	for _, ep := range []string{
		"http://127.0.0.1:9/hook",
		"http://10.0.0.5/hook",
		"http://169.254.169.254/latest",
		"http://metadata.google.internal/computeMetadata/v1/",
	} {
		if err := httpegress.Validate(ep); err == nil {
			t.Fatalf("expected reject for %s", ep)
		}
	}
}

func TestValidateAllowlistStillRejectsPrivate(t *testing.T) {
	t.Setenv(httpegress.EnvHTTPEgress, "1")
	t.Setenv(httpegress.EnvHTTPAllowlist, "http://10.0.0.5/hook,http://127.0.0.1:9/x,http://169.254.169.254/latest")
	for _, ep := range []string{
		"http://10.0.0.5/hook",
		"http://127.0.0.1:9/x",
		"http://169.254.169.254/latest",
	} {
		if err := httpegress.Validate(ep); err == nil {
			t.Fatalf("allowlist must not short-circuit private for %s", ep)
		}
	}
}

func TestValidateAllowlistPublicHostname(t *testing.T) {
	publicHook := "https://example.com/noctaxris-gcp-hook"
	t.Setenv(httpegress.EnvHTTPEgress, "")
	t.Setenv(httpegress.EnvHTTPAllowlist, publicHook)
	if err := httpegress.Validate(publicHook); err == nil {
		t.Fatal("allowlist without egress must fail")
	}
	t.Setenv(httpegress.EnvHTTPEgress, "1")
	if err := httpegress.Validate(publicHook); err != nil {
		t.Fatal(err)
	}
	if err := httpegress.Validate("https://example.com/other"); err == nil {
		t.Fatal("exact allowlist mismatch must fail")
	}
}

func TestClientRejectsRedirects(t *testing.T) {
	t.Parallel()
	client := httpegress.Client(0)
	if client.CheckRedirect == nil {
		t.Fatal("CheckRedirect must be set")
	}
	err := client.CheckRedirect(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("expected redirect denial, got %v", err)
	}
}
