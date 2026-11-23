// Package sharelink is the one definition of a PikPak share address: the
// domains that serve shares, the /s/<id> path, and the shape of an id. The
// API client, the web search and the route handlers all decide "is this a
// share, and which one" from here, so a new domain or a tightened rule does
// not have to be repeated in each of them.
package sharelink

import (
	"net/url"
	"regexp"
	"strings"
)

// Hosts are the domains PikPak serves share pages from, in the order a query
// tries them. The same share is served under every host, so the first is the
// one worth starting on; mypikpak.me no longer resolves, so it comes last.
var Hosts = []string{"mypikpak.com", "mypikpak.net", "pikpak.me", "mypikpak.me"}

// idPattern is the shape PikPak gives a share id.
var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{6,64}$`)

// Host reports whether host is one of the share domains or a subdomain of
// one (www.mypikpak.com, for example).
func Host(host string) bool {
	host = strings.ToLower(host)
	for _, site := range Hosts {
		if host == site || strings.HasSuffix(host, "."+site) {
			return true
		}
	}
	return false
}

// OnHost reports whether raw names a page on one of the share domains.
func OnHost(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return Host(u.Hostname())
}

// ID returns the share id when raw names a share page on one of the domains.
// It does not check the id's shape: a search lists what the provider gave,
// and opening the share validates it.
func ID(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || !Host(u.Hostname()) {
		return "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) >= 2 && parts[0] == "s" && parts[1] != "" {
		return parts[1], true
	}
	return "", false
}

// ValidID reports whether id has the shape PikPak uses for a share.
func ValidID(id string) bool { return idPattern.MatchString(id) }
