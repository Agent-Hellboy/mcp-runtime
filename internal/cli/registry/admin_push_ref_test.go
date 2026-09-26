package registry

import "testing"

func TestAdminPushPullRefRewritesClusterIPToPublicRegistry(t *testing.T) {
	cases := []struct {
		name, target, host, want string
		ok                       bool
	}{
		{
			name:   "cluster ip helper destination",
			target: "10.43.63.79:5000/org/buddy-mcp/buddy:bb82270",
			host:   "registry.example.com",
			want:   "registry.example.com/org/buddy-mcp/buddy:bb82270",
			ok:     true,
		},
		{
			name:   "scheme and trailing slash on host",
			target: "registry.registry.svc.cluster.local:5000/acme/demo:v1",
			host:   "https://registry.example.com/",
			want:   "registry.example.com/acme/demo:v1",
			ok:     true,
		},
		{
			name:   "already public",
			target: "registry.example.com/acme/demo:v1",
			host:   "registry.example.com",
			want:   "registry.example.com/acme/demo:v1",
		},
		{
			name:   "no public host known",
			target: "10.43.63.79:5000/acme/demo:v1",
			want:   "10.43.63.79:5000/acme/demo:v1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := adminPushPullRef(tc.target, tc.host)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("adminPushPullRef(%q, %q) = %q, %v; want %q, %v", tc.target, tc.host, got, ok, tc.want, tc.ok)
			}
		})
	}
}
