package blueprint

import "strings"

// SameRepo reports whether two clone URLs name the same repository: equal in
// host and path once lowercased and stripped of scheme, user, a trailing
// slash and ".git", so an SSH and an HTTPS URL of one repo match. The repo
// name alone never matches, and an empty URL matches nothing.
func SameRepo(a, b string) bool {
	na := normalizeURL(a)
	return na != "" && na == normalizeURL(b)
}

// normalizeURL reduces a clone URL to lowercase "host/path".
func normalizeURL(url string) string {
	u := strings.ToLower(strings.TrimSpace(url))
	if _, rest, ok := strings.Cut(u, "://"); ok {
		u = rest
	}
	if at := strings.Index(u, "@"); at >= 0 && at < strings.IndexAny(u+"/", "/") {
		u = u[at+1:]
	}
	if host, path, ok := strings.Cut(u, ":"); ok && !strings.Contains(host, "/") {
		u = host + "/" + withoutPort(path)
	}
	u = strings.TrimRight(u, "/")
	return strings.TrimSuffix(u, ".git")
}

// withoutPort drops a leading numeric port, as in host:22/path.
func withoutPort(path string) string {
	port, rest, ok := strings.Cut(path, "/")
	if ok && port != "" && strings.Trim(port, "0123456789") == "" {
		return rest
	}
	return path
}
