package tlsutil

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"testing"
	"time"
)

func TestDefaultCloudHostSANsIncludeProwlerHosts(t *testing.T) {
	sans := DefaultCloudHostSANs()
	want := map[string]bool{
		"cloudresourcemanager.googleapis.com": true,
		"iam.googleapis.com":                  true,
		"serviceusage.googleapis.com":         true,
		"storage.googleapis.com":              true,
		"compute.googleapis.com":              true,
		"logging.googleapis.com":              true,
		"iamcredentials.googleapis.com":       true,
	}
	for _, s := range sans {
		delete(want, s)
	}
	if len(want) > 0 {
		t.Fatalf("missing SANs: %v", want)
	}
	if !AllowedCloudHost("compute.googleapis.com") || !AllowedCloudHost("127.0.0.1:443") {
		t.Fatal("allowed host")
	}
	if AllowedCloudHost("evil.example") {
		t.Fatal("unexpected host")
	}
}

func TestEnsureServerCertWritesSANCert(t *testing.T) {
	dir := t.TempDir()
	p, err := EnsureServerCert(dir, time.Now().UTC().Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(p.ServerCert)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		t.Fatal("pem")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range cert.DNSNames {
		if n == "cloudresourcemanager.googleapis.com" {
			found = true
		}
	}
	if !found {
		t.Fatalf("dns names: %v", cert.DNSNames)
	}
	if _, err := LoadTLSConfig(p); err != nil {
		t.Fatal(err)
	}
	p2, err := EnsureServerCert(dir, time.Time{})
	if err != nil || p2.ServerCert != p.ServerCert {
		t.Fatal(err)
	}
}
