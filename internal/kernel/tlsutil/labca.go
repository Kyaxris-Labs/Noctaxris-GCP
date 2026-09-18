package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// DefaultCloudHostSANs are googleapis hostnames Prowler and similar tools call on 443.
func DefaultCloudHostSANs() []string {
	return []string{
		"cloudresourcemanager.googleapis.com",
		"iam.googleapis.com",
		"serviceusage.googleapis.com",
		"storage.googleapis.com",
		"compute.googleapis.com",
		"logging.googleapis.com",
		"iamcredentials.googleapis.com",
		"oauth2.googleapis.com",
		"www.googleapis.com",
		"localhost",
	}
}

// AllowedCloudHost reports whether host (SNI or Host header) is a lab cloud SAN.
func AllowedCloudHost(host string) bool {
	h, _, err := net.SplitHostPort(host)
	if err == nil {
		host = h
	}
	if host == "127.0.0.1" || host == "::1" {
		return true
	}
	for _, san := range DefaultCloudHostSANs() {
		if host == san {
			return true
		}
	}
	return false
}

// Paths holds generated lab CA and server PEMs.
type Paths struct {
	CACert     string
	ServerCert string
	ServerKey  string
}

// DefaultPaths returns PEM paths under secretsDir.
func DefaultPaths(secretsDir string) Paths {
	return Paths{
		CACert:     filepath.Join(secretsDir, "lab-ca.crt"),
		ServerCert: filepath.Join(secretsDir, "cloud-hosts.crt"),
		ServerKey:  filepath.Join(secretsDir, "cloud-hosts.key"),
	}
}

// EnsureServerCert writes a lab CA and server cert with cloud-host SANs when missing.
func EnsureServerCert(secretsDir string, notAfter time.Time) (Paths, error) {
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		return Paths{}, err
	}
	p := DefaultPaths(secretsDir)
	if fileExists(p.ServerCert) && fileExists(p.ServerKey) && fileExists(p.CACert) {
		return p, nil
	}
	if notAfter.IsZero() {
		notAfter = time.Now().UTC().Add(365 * 24 * time.Hour)
	}
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Paths{}, err
	}
	caSerial, err := randSerial()
	if err != nil {
		return Paths{}, err
	}
	caTpl := &x509.Certificate{
		SerialNumber:          caSerial,
		Subject:               pkix.Name{CommonName: "Noctaxris-GCP Lab CA", Organization: []string{"Noctaxris-GCP"}},
		NotBefore:             time.Now().UTC().Add(-time.Hour),
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTpl, caTpl, &caKey.PublicKey, caKey)
	if err != nil {
		return Paths{}, err
	}
	srvKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Paths{}, err
	}
	srvSerial, err := randSerial()
	if err != nil {
		return Paths{}, err
	}
	sans := DefaultCloudHostSANs()
	srvTpl := &x509.Certificate{
		SerialNumber: srvSerial,
		Subject:      pkix.Name{CommonName: "cloudresourcemanager.googleapis.com"},
		NotBefore:    time.Now().UTC().Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     sans,
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return Paths{}, err
	}
	srvDER, err := x509.CreateCertificate(rand.Reader, srvTpl, caCert, &srvKey.PublicKey, caKey)
	if err != nil {
		return Paths{}, err
	}
	if err := writePEM(p.CACert, "CERTIFICATE", caDER, 0o644); err != nil {
		return Paths{}, err
	}
	if err := writePEM(p.ServerCert, "CERTIFICATE", srvDER, 0o644); err != nil {
		return Paths{}, err
	}
	keyDER, err := x509.MarshalECPrivateKey(srvKey)
	if err != nil {
		return Paths{}, err
	}
	if err := writePEM(p.ServerKey, "EC PRIVATE KEY", keyDER, 0o600); err != nil {
		return Paths{}, err
	}
	return p, nil
}

// LoadTLSConfig loads the generated server cert for SNI listeners.
func LoadTLSConfig(p Paths) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(p.ServerCert, p.ServerKey)
	if err != nil {
		return nil, fmt.Errorf("load cloud-hosts cert: %w", err)
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}, nil
}

func writePEM(path, typ string, der []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: typ, Bytes: der})
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Size() > 0
}

func randSerial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
}
