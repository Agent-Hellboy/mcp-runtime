package cluster

// IngressOptions captures ingress install settings used by both cluster
// configuration and the setup command.
type IngressOptions struct {
	Mode     string
	Manifest string
	Force    bool
}
