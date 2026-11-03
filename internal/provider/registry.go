package provider

import "sort"

// Registry maps platform names to providers. It knows no concrete platform; the daemon
// builds it from config.
type Registry map[string]Provider

// Get returns the provider for platform.
func (r Registry) Get(platform string) (Provider, bool) {
	p, ok := r[platform]
	return p, ok
}

// Platforms returns the registered platform names, sorted.
func (r Registry) Platforms() []string {
	out := make([]string, 0, len(r))
	for k := range r {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
