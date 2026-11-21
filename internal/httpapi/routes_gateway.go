package httpapi

import (
	"net/http"
	"sort"
	"strings"

	"fnos-pikpak/internal/fnos"
	"fnos-pikpak/internal/probe"
)

func registerGatewayRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/gateway/info", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())

		counts := map[string]int{}
		for name, values := range r.Header {
			lower := strings.ToLower(name)
			if strings.HasPrefix(lower, "x-trim-") ||
				strings.HasPrefix(lower, "x-forwarded-") ||
				lower == "origin" || lower == "referer" {
				counts[name] = len(values)
			}
		}

		rawQuery := ""
		if r.URL.RawQuery != "" {
			rawQuery = probe.Redact("?" + r.URL.RawQuery)
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"path":              r.URL.Path,
			"raw_query":         rawQuery,
			"origin":            r.Header.Get("Origin"),
			"referer":           probe.Redact(r.Header.Get("Referer")),
			"host":              r.Host,
			"forwarded_headers": counts,
			"header_names":      sortedKeys(counts),
			"identity": map[string]any{
				"uid":      id.UID,
				"username": id.Username,
				"is_admin": id.IsAdmin,
			},
		})
	})
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// forgedTest reports how many identity headers arrived. It deliberately does
// not parse or use any uid: it only answers "did the gateway strip a forged
// client copy?".
func forgedTest() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		names := r.Header.Values("X-Trim-Userid")
		writeJSON(w, http.StatusOK, map[string]any{
			"header_count":       len(names),
			"duplicate_detected": len(names) > 1,
			"note":               "this endpoint does not authenticate or act on the header",
		})
	}
}
