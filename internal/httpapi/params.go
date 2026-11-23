package httpapi

import (
	"net/http"
	"strconv"
)

// pathID reads a numeric id from a path segment, answering 400 bad_ids when it
// is missing, not a number, or not positive. Every favourite route takes its
// id this way, so the refusal is one message rather than five copies.
func pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "bad_ids", "收藏编号不合法")
		return 0, false
	}
	return id, true
}
