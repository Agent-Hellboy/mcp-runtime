package ref

import "testing"

func TestSplitImage(t *testing.T) {
	tests := []struct {
		image string
		want  string
		tag   string
	}{
		{"registry.example.com/example-mcp-server:latest", "registry.example.com/example-mcp-server", "latest"},
		{"registry.example.com/example-mcp-server", "registry.example.com/example-mcp-server", ""},
		{"example-mcp-server:latest", "example-mcp-server", "latest"},
		{"example-mcp-server", "example-mcp-server", ""},
	}
	for _, test := range tests {
		image, tag := SplitImage(test.image)
		if image != test.want {
			t.Errorf("SplitImage(%q) = %q, want %q", test.image, image, test.want)
		}
		if tag != test.tag {
			t.Errorf("SplitImage(%q) tag = %q, want %q", test.image, tag, test.tag)
		}
	}
}

func TestDropRegistryPrefix(t *testing.T) {
	tests := []struct {
		repo string
		want string
	}{
		{"registry.example.com/example-mcp-server", "example-mcp-server"},
		{"example-mcp-server", "example-mcp-server"},
		{"localhost:5000/my-image", "my-image"},
		{"192.168.1.1:5000/my-image", "my-image"},
		{"my-image", "my-image"},
		{"user/repo", "user/repo"},
		{"gcr.io/project/image", "project/image"},
		{"docker.io/library/nginx", "library/nginx"},
	}
	for _, test := range tests {
		repo := DropRegistryPrefix(test.repo)
		if repo != test.want {
			t.Errorf("DropRegistryPrefix(%q) = %q, want %q", test.repo, repo, test.want)
		}
	}
}
