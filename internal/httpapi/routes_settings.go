package httpapi

import (
	"net/http"
	"path/filepath"
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

// validAPIKey accepts an empty value (clear it) or a provider token: no
// whitespace, so a pasted line break cannot corrupt the key.
func validAPIKey(s string) bool {
	if s == "" {
		return true
	}
	if len(s) < 8 || len(s) > 200 {
		return false
	}
	for _, r := range s {
		if r <= ' ' || r == 0x7f {
			return false
		}
	}
	return true
}

var settingSpecs = map[string]settingSpec{
	"download_concurrency": {def: "2", valid: intInRange(1, 4)},
	"upload_concurrency":   {def: "2", valid: intInRange(1, 4)},
	"conflict":             {def: "rename", valid: oneOf("rename", "skip", "overwrite")},
	"play_mode":            {def: "proxy", valid: oneOf("proxy", "redirect")},
	// player picks the macOS external player once, instead of asking on every
	// play: builtin keeps 预览/完整播放 in the app, iina hands the original
	// CDN link to IINA.
	"player": {def: "builtin", valid: oneOf("builtin", "iina")},
	// tile_size picks the icon view's tile: small is the fnOS default,
	// medium is 1.5x and large 2x its width and height.
	"tile_size": {def: "small", valid: oneOf("small", "medium", "large")},
	"language":  {def: "zh-CN", valid: oneOf("zh-CN", "en")},
	// Empty means "not set yet". Whether the directory may be written is a
	// question only fnOS can answer, so it is checked separately.
	"download_dir": {def: "", valid: func(s string) bool { return s == "" || filepath.IsAbs(s) }},
	// The 全网搜索 provider key. It is global (one key serves every fnOS user)
	// and secret: reads report only whether one is stored.
	"search_api_key": {def: "", valid: validAPIKey},
}

// globalOnlySettings may be written and read through /api/settings only; a
// /api/user-settings call cannot see or touch them.
var globalOnlySettings = map[string]bool{
	"search_api_key": true,
}

// secretSettings are stored but never echoed: a read says "_set" instead.
var secretSettings = map[string]bool{
	"search_api_key": true,
}

func registerSettingsRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, readSettings(r, deps.Store, 0))
	})
	mux.HandleFunc("PUT /api/settings", func(w http.ResponseWriter, r *http.Request) {
		writeSettings(w, r, deps, 0)
	})
	mux.HandleFunc("GET /api/user-settings", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())
		writeJSON(w, http.StatusOK, readSettings(r, deps.Store, id.UID))
	})
	mux.HandleFunc("PUT /api/user-settings", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())
		writeSettings(w, r, deps, id.UID)
	})
}

// readSettings returns every known key, falling back to its default when the
// row is absent. uid 0 means the global table.
func readSettings(r *http.Request, st *store.Store, uid int) map[string]string {
	out := make(map[string]string, len(settingSpecs))
	for key, spec := range settingSpecs {
		if uid != 0 && globalOnlySettings[key] {
			continue
		}
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
		if secretSettings[key] {
			out[key+"_set"] = "0"
			if v != "" {
				out[key+"_set"] = "1"
			}
			continue
		}
		out[key] = v
	}
	return out
}

// writeSettings validates the whole body first, so a rejected key cannot leave
// a half-applied update. It reports whether the response was written.
func writeSettings(w http.ResponseWriter, r *http.Request, deps Deps, uid int) bool {
	st := deps.Store
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
		if uid != 0 && globalOnlySettings[key] {
			writeError(w, http.StatusBadRequest, "not_user_setting", "设置项 "+key+" 属于全局设置，不能在个人设置里修改")
			return false
		}
		if !spec.valid(value) {
			writeError(w, http.StatusBadRequest, "invalid_setting", "设置项 "+key+" 的值不合法")
			return false
		}
	}
	// A download directory must be one this user may write to, which only the
	// fnOS guard can decide.
	if dir, ok := body["download_dir"]; ok && uid != 0 && dir != "" {
		if err := checkDir(r, deps, uid, dir, true); err != nil {
			writeDirError(w, err)
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
