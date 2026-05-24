package mtggoldfish

import "strings"

// joinURL composes an absolute URL from a base and an absolute-or-relative path.
func joinURL(base, path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	base = strings.TrimSuffix(base, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}
