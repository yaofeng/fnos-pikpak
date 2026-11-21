package httpapi

import (
	"net/http"
	"strconv"

	"fnos-pikpak/internal/fnos"
	"fnos-pikpak/internal/store"
)

// settingSpec is the allow-list the settings endpoints enforce. Values are
// strings on the wire so one validator serves both numeric and enum keys.
type settingSpec struct {
	def   string
	valid func(string) bool
}

func intInRange(lo, hi int) func(string) bool {
	return func(s string) bool {
		n, err := strconv.Atoi(s)
		return err == nil && n >= lo && n <= hi
	}
}

func oneOf(values ...string) func(string) bool {
	return func(s string) bool {
		for _, v := range values {
			if s == v {
				return true
			}
		}
		return false
	}
}

var settingSpecs = map[string]settingSpec{
	"download_concurrency": {def: "2", valid: intInRange(1, 4)},
	"upload_concurrency":   {def: "2", valid: intInRange(1, 4)},
	"conflict":             {def: "rename", valid: oneOf("rename", "skip", "overwrite")},
	"play_mode":            {def: "proxy", valid: oneOf("proxy", "redirect")},
	"language":             {def: "zh-CN", valid: oneOf("zh-CN", "en")},
}

func registerSettingsRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, readSettings(r, deps.Store, 0))
	})
	mux.HandleFunc("PUT /api/settings", func(w http.ResponseWriter, r *http.Request) {
		writeSettings(w, r, deps.Store, 0)
	})
	mux.HandleFunc("GET /api/user-settings", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())
		writeJSON(w, http.StatusOK, readSettings(r, deps.Store, id.UID))
	})
	mux.HandleFunc("PUT /api/user-settings", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())
		writeSettings(w, r, deps.Store, id.UID)
	})
}

// readSettings returns every known key, falling back to its default when the
// row is absent. uid 0 means the global table.
func readSettings(r *http.Request, st *store.Store, uid int) map[string]string {
	out := make(map[string]string, len(settingSpecs))
	for key, spec := range settingSpecs {
		v := ""
		var err error
		if uid == 0 {
			v, err = st.GetSetting(r.Context(), key)
		} else {
			v, err = st.GetUserSetting(r.Context(), uid, key)
		}
		if err != nil || !spec.valid(v) {
			v = spec.def
		}
		out[key] = v
	}
	return out
}

// writeSettings validates the whole body first, so a rejected key cannot leave
// a half-applied update. It reports whether the response was written.
func writeSettings(w http.ResponseWriter, r *http.Request, st *store.Store, uid int) bool {
	var body map[string]string
	if err := decodeJSON(w, r, &body); err != nil {
		return false
	}
	if len(body) == 0 {
		writeError(w, http.StatusBadRequest, "empty_body", "没有需要更新的设置")
		return false
	}
	for key, value := range body {
		spec, ok := settingSpecs[key]
		if !ok {
			writeError(w, http.StatusBadRequest, "unknown_setting", "未知的设置项："+key)
			return false
		}
		if !spec.valid(value) {
			writeError(w, http.StatusBadRequest, "invalid_setting", "设置项 "+key+" 的值不合法")
			return false
		}
	}
	for key, value := range body {
		var err error
		if uid == 0 {
			err = st.SetSetting(r.Context(), key, value)
		} else {
			err = st.SetUserSetting(r.Context(), uid, key, value)
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "保存设置失败")
			return false
		}
	}
	writeJSON(w, http.StatusOK, readSettings(r, st, uid))
	return true
}
