package config

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/compute"
)

const (
	// EnvAllowNonLoopbackListen permits binding non-loopback addresses without TLS
	// (Compose container bind with host publish restricted to 127.0.0.1).
	EnvAllowNonLoopbackListen = "NOCTAXRIS_GCP_ALLOW_NONLOOPBACK_LISTEN"

	DefaultListenAddr = "127.0.0.1:4588"
	DefaultDataRoot   = "/var/lib/noctaxris-gcp"
	DefaultProjectID  = "noctaxris-gcp-local"

	// EnvLabForensics enables lab FreezeClock / UnfreezeClock / SetClock / BulkSeed.
	EnvLabForensics = "NOCTAXRIS_GCP_LAB_FORENSICS"
	// EnvLogsInject enables lab POST /_noctaxris-gcp/lab/logs:inject (non-CAL log planes).
	EnvLogsInject = "NOCTAXRIS_GCP_LOGS_INJECT"
	// EnvCloudHosts enables the second loopback TLS listener with googleapis SANs.
	EnvCloudHosts = "NOCTAXRIS_GCP_CLOUD_HOSTS"
	// EnvStripProduct replaces product branding on ops health paths, root SA display name, and lab CA subject.
	EnvStripProduct = "NOCTAXRIS_GCP_STRIP_PRODUCT"

	DefaultCloudHostsListenAddr = "127.0.0.1:8443"

	OpsPathPrefixProduct = "/_noctaxris-gcp"
	OpsPathPrefixLab     = "/_lab"

	RootSADisplayNameProduct = "Noctaxris-GCP root"
	RootSADisplayNameStrip   = "Root"

	LabCACommonNameProduct = "Noctaxris-GCP Lab CA"
	LabCACommonNameStrip   = "Lab CA"
	LabCAOrgProduct        = "Noctaxris-GCP"
	LabCAOrgStrip          = "Lab"
)

// Shipped docker/.env.example root pair. Refused when listen is non-loopback.
const (
	exampleRootServiceAccount = "root@example.iam.gserviceaccount.com"
	exampleRootAccessToken    = "noctaxris-gcp-example-root-token"
)

// Config holds process configuration loaded from NOCTAXRIS_GCP_* environment variables.
type Config struct {
	ListenAddr             string
	DataRoot               string
	MasterKeyPath          string
	TLSCertFile            string
	TLSKeyFile             string
	RootServiceAccount     string
	RootAccessToken        string
	ProjectID              string
	AllowNonLoopbackListen bool
	// DockerHost is the nested DinD engine URL (NOCTAXRIS_GCP_DOCKER_HOST).
	// Empty disables nested compute so unit tests run without Docker.
	DockerHost string
	// DockerTLSCertPath is the directory with ca.pem, cert.pem, key.pem for engine TLS.
	DockerTLSCertPath string
	// LabForensics enables /_noctaxris-gcp/lab clock and BulkSeed routes (default off).
	LabForensics bool
	// LogsInject enables /_noctaxris-gcp/lab/logs:inject (default off).
	LogsInject bool
	// CloudHosts starts a second TLS listener with googleapis DNS SANs (default off).
	CloudHosts bool
	// CloudHostsListen is the TLS bind when CloudHosts is on.
	CloudHostsListen string
	// StripProduct uses /_lab ops paths and generic CA/root labels (default off).
	StripProduct bool
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
		DockerHost:             getenv("NOCTAXRIS_GCP_DOCKER_HOST", ""),
		DockerTLSCertPath:      getenv("NOCTAXRIS_GCP_DOCKER_CERT_PATH", ""),
		LabForensics:           envTruthy(EnvLabForensics),
		LogsInject:             envTruthy(EnvLogsInject),
		CloudHosts:             envTruthy(EnvCloudHosts),
		CloudHostsListen:       getenv("NOCTAXRIS_GCP_CLOUD_HOSTS_LISTEN", ""),
		StripProduct:           envTruthy(EnvStripProduct),
	}
	if cfg.CloudHosts && strings.TrimSpace(cfg.CloudHostsListen) == "" {
		cfg.CloudHostsListen = DefaultCloudHostsListenAddr
	}
	if err := compute.ValidateDockerHost(cfg.DockerHost, cfg.DockerTLSCertPath); err != nil {
		return Config{}, err
	}
	if err := ValidateListenSecurity(cfg); err != nil {
		return Config{}, err
	}
	if err := ValidateCloudHostsListen(cfg); err != nil {
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

// OpsPathPrefix returns the ops health/version path prefix.
func (c Config) OpsPathPrefix() string {
	return OpsPathPrefix(c.StripProduct)
}

// OpsPathPrefix returns /_noctaxris-gcp or /_lab when stripProduct is set.
func OpsPathPrefix(stripProduct bool) string {
	if stripProduct {
		return OpsPathPrefixLab
	}
	return OpsPathPrefixProduct
}

// HealthPaths returns liveness, readiness, and version paths for the ops prefix.
func (c Config) HealthPaths() (health, ready, version string) {
	return HealthPaths(c.StripProduct)
}

// HealthPaths returns liveness, readiness, and version paths.
func HealthPaths(stripProduct bool) (health, ready, version string) {
	p := OpsPathPrefix(stripProduct)
	return p + "/health", p + "/ready", p + "/version"
}

// RootSADisplayName returns the EnsureRoot service account display name.
func (c Config) RootSADisplayName() string {
	return RootSADisplayName(c.StripProduct)
}

// RootSADisplayName returns product or strip root SA display name.
func RootSADisplayName(stripProduct bool) string {
	if stripProduct {
		return RootSADisplayNameStrip
	}
	return RootSADisplayNameProduct
}

// LabCACommonName returns the lab CA certificate common name.
func (c Config) LabCACommonName() string {
	return LabCACommonName(c.StripProduct)
}

// LabCACommonName returns product or strip lab CA common name.
func LabCACommonName(stripProduct bool) string {
	if stripProduct {
		return LabCACommonNameStrip
	}
	return LabCACommonNameProduct
}

// LabCAOrganization returns the lab CA certificate organization.
func (c Config) LabCAOrganization() string {
	return LabCAOrganization(c.StripProduct)
}

// LabCAOrganization returns product or strip lab CA organization.
func LabCAOrganization(stripProduct bool) string {
	if stripProduct {
		return LabCAOrgStrip
	}
	return LabCAOrgProduct
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

// ValidateCloudHostsListen fails closed for non-loopback cloud-hosts TLS without allow.
func ValidateCloudHostsListen(c Config) error {
	if !c.CloudHosts {
		return nil
	}
	if ListenIsLoopback(c.CloudHostsListen) {
		return nil
	}
	if c.AllowNonLoopbackListen || envTruthy(EnvAllowNonLoopbackListen) {
		return nil
	}
	return fmt.Errorf("NOCTAXRIS_GCP_CLOUD_HOSTS_LISTEN %q is non-loopback; keep loopback or set %s=1 when host publish stays loopback",
		c.CloudHostsListen, EnvAllowNonLoopbackListen)
}
