package config

import (
	"fmt"
	"net"
	"os"
	"strings"
)

const (
	// EnvAllowNonLoopbackListen permits binding non-loopback addresses without TLS
	// (Compose container bind with host publish restricted to 127.0.0.1).
	EnvAllowNonLoopbackListen = "NOCTAXRIS_GCP_ALLOW_NONLOOPBACK_LISTEN"

	DefaultListenAddr = "127.0.0.1:4588"
	DefaultDataRoot   = "/var/lib/noctaxris-gcp"
	DefaultProjectID  = "noctaxris-gcp-local"
)

// Shipped docker/.env.example root pair. Refused when listen is non-loopback.
const (
	exampleRootServiceAccount = "root@example.iam.gserviceaccount.com"
	exampleRootAccessToken    = "noctaxris-gcp-example-root-token"
)

// Config holds process configuration loaded from NOCTAXRIS_GCP_* environment variables.
type Config struct {
	ListenAddr           string
	DataRoot             string
	MasterKeyPath        string
	TLSCertFile          string
	TLSKeyFile           string
	RootServiceAccount   string
	RootAccessToken      string
	ProjectID            string
	AllowNonLoopbackListen bool
}

// LoadFromEnv reads configuration from the process environment.
func LoadFromEnv() (Config, error) {
	cfg := Config{
		ListenAddr:             getenv("NOCTAXRIS_GCP_LISTEN", DefaultListenAddr),
		DataRoot:               getenv("NOCTAXRIS_GCP_DATA_ROOT", DefaultDataRoot),
		MasterKeyPath:          getenv("NOCTAXRIS_GCP_MASTER_KEY_FILE", ""),
		TLSCertFile:            getenv("NOCTAXRIS_GCP_TLS_CERT", ""),
		TLSKeyFile:             getenv("NOCTAXRIS_GCP_TLS_KEY", ""),
		RootServiceAccount:     getenv("NOCTAXRIS_GCP_ROOT_SERVICE_ACCOUNT", ""),
		RootAccessToken:        getenv("NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN", ""),
		ProjectID:              getenv("NOCTAXRIS_GCP_PROJECT", DefaultProjectID),
		AllowNonLoopbackListen: envTruthy(EnvAllowNonLoopbackListen),
	}
	if err := ValidateListenSecurity(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envTruthy(key string) bool {
	return strings.EqualFold(os.Getenv(key), "1") ||
		strings.EqualFold(os.Getenv(key), "true")
}

// ExampleRootCredentials reports whether sa and token match the shipped .env.example pair.
func ExampleRootCredentials(sa, token string) bool {
	return sa == exampleRootServiceAccount && token == exampleRootAccessToken
}

// ListenIsLoopback reports whether addr binds only loopback.
// Empty host / ":port" / 0.0.0.0 / :: are non-loopback (all-interfaces bind).
func ListenIsLoopback(addr string) bool {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return false
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		if strings.HasPrefix(addr, ":") {
			return false
		}
		host = addr
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

// TLSEnabled reports whether both TLS PEM paths are set.
func (c Config) TLSEnabled() bool {
	return strings.TrimSpace(c.TLSCertFile) != "" && strings.TrimSpace(c.TLSKeyFile) != ""
}

// ValidateListenSecurity fails closed when listen is a concrete non-loopback
// address without TLS, unless NOCTAXRIS_GCP_ALLOW_NONLOOPBACK_LISTEN is set.
func ValidateListenSecurity(c Config) error {
	if ListenIsLoopback(c.ListenAddr) {
		return nil
	}
	if c.TLSEnabled() {
		return nil
	}
	if c.AllowNonLoopbackListen || envTruthy(EnvAllowNonLoopbackListen) {
		return nil
	}
	return fmt.Errorf("NOCTAXRIS_GCP_LISTEN %q is non-loopback without TLS; set NOCTAXRIS_GCP_TLS_CERT and NOCTAXRIS_GCP_TLS_KEY, or %s=1 when host publish stays loopback (Compose)",
		c.ListenAddr, EnvAllowNonLoopbackListen)
}
