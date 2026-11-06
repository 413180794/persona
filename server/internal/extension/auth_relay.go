package extension

import (
	"net/url"
	"slices"
	"sort"
	"strings"
)

// AuthRelayNames says whether the auth_relay contribution a names link: https, no user or port, one of its hosts and
// one of its paths (a trailing / aside). What is in the query is the extension's to judge.
func AuthRelayNames(a AuthRelayItem, link string) bool {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Opaque != "" {
		return false
	}
	if !slices.Contains(a.Hosts, strings.ToLower(u.Hostname())) {
		return false
	}
	path := trimSlash(u.Path)
	return slices.ContainsFunc(a.Paths, func(p string) bool { return trimSlash(p) == path })
}

func trimSlash(p string) string {
	if t := strings.TrimRight(p, "/"); t != "" {
		return t
	}
	return "/"
}

// AuthRelayer is a contribution to auth_relay that names a link.
type AuthRelayer struct {
	Extension    string
	Contribution string
}

// AuthRelayers snapshots the enabled, compatible extensions granted auth_relay whose contributions (the ones switched
// on) name link, by extension id.
func (r *Registry) AuthRelayers(link string) []AuthRelayer {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []AuthRelayer
	for id, p := range r.extensions {
		if !p.Record.Enabled || p.Problem != "" || p.Manifest == nil || !p.Record.Granted.AuthRelay {
			continue
		}
		for _, it := range p.Manifest.AuthRelays() {
			if AuthRelayNames(it, link) {
				out = append(out, AuthRelayer{Extension: id, Contribution: it.ID})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Extension != out[j].Extension {
			return out[i].Extension < out[j].Extension
		}
		return out[i].Contribution < out[j].Contribution
	})
	return out
}
