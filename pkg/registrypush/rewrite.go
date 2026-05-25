package registrypush

import (
	"fmt"
	"strconv"
	"strings"
)

// Hosts identifies bundled registry hostnames that should be rewritten to an
// in-cluster service DNS name for skopeo pushes from inside the cluster.
type Hosts struct {
	InternalHostnames []string
	ServiceName       string
	ServiceNamespace  string
	ServicePort       int
}

// RewritePushTarget replaces the registry host in target when it matches a
// bundled internal registry hostname. Targets already using cluster DNS or
// external registries are returned unchanged.
func RewritePushTarget(target string, hosts Hosts) string {
	slash := strings.Index(target, "/")
	if slash <= 0 {
		return target
	}
	host := target[:slash]
	rest := target[slash:]

	lowerHost := strings.ToLower(host)
	if strings.Contains(lowerHost, ".svc.cluster.local") {
		return target
	}

	hostNoPort := lowerHost
	if idx := strings.LastIndex(hostNoPort, ":"); idx >= 0 {
		hostNoPort = hostNoPort[:idx]
	}

	internal := map[string]struct{}{}
	for _, value := range hosts.InternalHostnames {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if idx := strings.LastIndex(value, ":"); idx >= 0 {
			value = value[:idx]
		}
		if value != "" {
			internal[value] = struct{}{}
		}
	}
	if _, ok := internal[hostNoPort]; !ok {
		return target
	}

	serviceName := strings.TrimSpace(hosts.ServiceName)
	serviceNamespace := strings.TrimSpace(hosts.ServiceNamespace)
	port := hosts.ServicePort
	if serviceName == "" || serviceNamespace == "" || port <= 0 {
		return target
	}
	return fmt.Sprintf("%s.%s.svc.cluster.local:%d%s", serviceName, serviceNamespace, port, rest)
}

// ParsePortOrDefault parses a service port string or returns def.
func ParsePortOrDefault(value string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n <= 0 || n > 65535 {
		return def
	}
	return n
}
