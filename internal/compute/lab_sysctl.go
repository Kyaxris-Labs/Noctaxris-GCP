package compute

// LabNetSysctls disables IPv6 inside nested containers created on the engine.
// Docker Engine 29 on some DinD hosts fails start with
// "failed to disable IPv6 on container's interface eth0" when the daemon
// clears IPv6 on the veth; these create-time sysctls avoid that path.
// NetworkMode "none" does not need them.
func LabNetSysctls() map[string]string {
	return map[string]string{
		"net.ipv6.conf.all.disable_ipv6":     "1",
		"net.ipv6.conf.default.disable_ipv6": "1",
	}
}
