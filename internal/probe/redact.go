package probe

import "regexp"

var (
	bearerRE = regexp.MustCompile(`(?i)(bearer\s+)[\w.\-]+`)
	queryRE  = regexp.MustCompile(`\?[^\s"'<>]+`)
	kvRE     = regexp.MustCompile(`(?i)(access_token|refresh_token|device_code|pass_code_token|authorization)(["']?\s*[:=]\s*["']?)([^&\s"',}]+)`)
)

// Redact removes secrets from anything that can reach a log or the M0 report:
// URL query strings (signed download links), bearer tokens, and token fields.
func Redact(s string) string {
	s = bearerRE.ReplaceAllString(s, "${1}<redacted>")
	s = kvRE.ReplaceAllString(s, "${1}${2}<redacted>")
	s = queryRE.ReplaceAllString(s, "?<redacted>")
	return s
}
