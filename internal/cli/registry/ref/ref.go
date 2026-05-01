package ref

import "strings"

// SplitImage returns the repository/name portion and optional tag for an image reference.
func SplitImage(image string) (string, string) {
	tag := ""
	parts := strings.Split(image, ":")
	if len(parts) > 1 && !strings.Contains(parts[len(parts)-1], "/") {
		tag = parts[len(parts)-1]
		image = strings.Join(parts[:len(parts)-1], ":")
	}
	return image, tag
}

// DropRegistryPrefix removes an explicit registry host from an image repository.
func DropRegistryPrefix(repo string) string {
	parts := strings.Split(repo, "/")
	if len(parts) <= 1 {
		return repo
	}
	first := parts[0]
	if strings.Contains(first, ".") || strings.Contains(first, ":") || first == "localhost" {
		return strings.Join(parts[1:], "/")
	}
	return repo
}
