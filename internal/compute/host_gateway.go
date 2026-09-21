package compute

import (
	"os"
	"strings"
)

// EnvInjectHostGateway controls ExtraHosts host.docker.internal:host-gateway
// injection into nested Cloud Build step containers. Default is off; set to "1"
// or "true" so in-step clients can reach the host-published API on :4588.
const EnvInjectHostGateway = "NOCTAXRIS_GCP_INJECT_HOST_GATEWAY"

// HostGatewayExtraHosts returns ExtraHosts for Cloud Build DinD children, or nil when disabled.
// Default is nil; opt in with NOCTAXRIS_GCP_INJECT_HOST_GATEWAY=1.
func HostGatewayExtraHosts() []string {
	v := strings.TrimSpace(os.Getenv(EnvInjectHostGateway))
	if v == "1" || strings.EqualFold(v, "true") {
		return []string{"host.docker.internal:host-gateway"}
	}
	return nil
}
