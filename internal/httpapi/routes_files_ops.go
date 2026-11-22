package httpapi

import (
	"net/http"
	"strings"
	"time"
)

const maxBatchIDs = 200

// batchWaitTimeout bounds how long a write waits for PikPak's batch task
// before answering "pending". Tests shorten it.
var batchWaitTimeout = 10 * time.Second

// taskPollInterval is how often the waiter re-reads a task. It is a variable
// so tests do not pay real seconds.
var taskPollInterval = 300 * time.Millisecond

type idsRequest struct {
	IDs        []string `json:"ids"`
	ToParentID string   `json:"to_parent_id"`
}

// readIDs decodes and validates a batch body: 1–200 unique, non-blank ids.
func readIDs(w http.ResponseWriter, r *http.Request, needTarget bool) (idsRequest, bool) {
	var body idsRequest
	if err := decodeJSON(w, r, &body); err != nil {
		return idsRequest{}, false
	}
	seen := map[string]bool{}
	ids := make([]string, 0, len(body.IDs))
	for _, id := range body.IDs {
		id = strings.TrimSpace(id)
		if id == "" {
			writeError(w, http.StatusBadRequest, "bad_ids", "ids 不能包含空值")
			return idsRequest{}, false
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		writeError(w, http.StatusBadRequest, "bad_ids", "至少需要一个 id")
		return idsRequest{}, false
	}
	if len(ids) > maxBatchIDs {
		writeError(w, http.StatusBadRequest, "too_many_ids", "一次最多操作 200 项")
		return idsRequest{}, false
	}
	body.IDs = ids
	if needTarget {
		body.ToParentID = strings.TrimSpace(body.ToParentID)
		if body.ToParentID != "" && seen[body.ToParentID] {
			writeError(w, http.StatusBadRequest, "bad_target", "不能把文件夹移动到它自己里面")
			return idsRequest{}, false
		}
	}
	return body, true
}

func registerFileOperationRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("POST /api/folders", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ParentID string `json:"parent_id"`
			Name     string `json:"name"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			return
		}
		name := strings.TrimSpace(body.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "bad_name", "文件夹名称不能为空")
			return
		}
		client, ok := boundClient(w, r, deps)
		if !ok {
			return
		}
		file, err := client.CreateFolder(r.Context(), strings.TrimSpace(body.ParentID), name)
		if err != nil {
			writeUpstreamError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"file": file})
	})

	mux.HandleFunc("PATCH /api/files/{id}", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name string `json:"name"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			return
		}
		name := strings.TrimSpace(body.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "bad_name", "名称不能为空")
			return
		}
		client, ok := boundClient(w, r, deps)
		if !ok {
			return
		}
		if err := client.Rename(r.Context(), r.PathValue("id"), name); err != nil {
			writeUpstreamError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	registerBatch(mux, deps, "move", true)
	registerBatch(mux, deps, "copy", true)
	registerBatch(mux, deps, "trash", false)
	registerBatch(mux, deps, "untrash", false)
	registerBatch(mux, deps, "delete", false)

	mux.HandleFunc("POST /api/files/star", func(w http.ResponseWriter, r *http.Request) {
		body, ok := readIDs(w, r, false)
		if !ok {
			return
		}
		client, ok := boundClient(w, r, deps)
		if !ok {
			return
		}
		if err := client.Star(r.Context(), body.IDs); err != nil {
			writeUpstreamError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /api/files/unstar", func(w http.ResponseWriter, r *http.Request) {
		body, ok := readIDs(w, r, false)
		if !ok {
			return
		}
		client, ok := boundClient(w, r, deps)
		if !ok {
			return
		}
		if err := client.Unstar(r.Context(), body.IDs); err != nil {
			writeUpstreamError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
}

func registerBatch(mux *http.ServeMux, deps Deps, op string, needTarget bool) {
	mux.HandleFunc("POST /api/files/"+op, func(w http.ResponseWriter, r *http.Request) {
		body, ok := readIDs(w, r, needTarget)
		if !ok {
			return
		}
		client, ok := boundClient(w, r, deps)
		if !ok {
			return
		}

		var (
			taskID string
			err    error
		)
		switch op {
		case "move":
			taskID, err = client.BatchMove(r.Context(), body.IDs, body.ToParentID)
		case "copy":
			taskID, err = client.BatchCopy(r.Context(), body.IDs, body.ToParentID)
		case "trash":
			taskID, err = client.BatchTrash(r.Context(), body.IDs)
		case "untrash":
			taskID, err = client.BatchUntrash(r.Context(), body.IDs)
		case "delete":
			taskID, err = client.BatchDelete(r.Context(), body.IDs)
		}
		if err != nil {
			writeUpstreamError(w, err)
			return
		}

		resp := map[string]any{"ok": true, "pending": false}
		if taskID != "" {
			task, done, err := client.WaitTaskSettled(r.Context(), taskID, batchWaitTimeout, taskPollInterval)
			if err != nil {
				writeUpstreamError(w, err)
				return
			}
			resp["task_id"] = taskID
			resp["pending"] = !done
			if task.Phase != "" {
				resp["phase"] = task.Phase
			}
			if !done && task.Phase == "PHASE_TYPE_ERROR" {
				resp["ok"] = false
			}
		}
		writeJSON(w, http.StatusOK, resp)
	})
}
