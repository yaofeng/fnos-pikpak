package pikpak

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseShareLinkAcceptsTheFormsAPersonPastes(t *testing.T) {
	cases := []struct {
		in     string
		id     string
		pass   string
		okLink bool
	}{
		{in: "https://mypikpak.com/s/VOabc123", id: "VOabc123", okLink: true},
		{in: "https://www.mypikpak.com/s/VOabc123?pwd=7f3a", id: "VOabc123", pass: "7f3a", okLink: true},
		{in: "mypikpak.com/s/VOabc123", id: "VOabc123", okLink: true},
		{in: "VOabc123", id: "VOabc123", okLink: true},
		{in: "https://example.com/s/VOabc123", okLink: false},
		{in: "", okLink: false},
	}
	for _, c := range cases {
		id, pass, err := ParseShareLink(c.in)
		if !c.okLink {
			if err == nil {
				t.Fatalf("%q parsed as %q", c.in, id)
			}
			continue
		}
		if err != nil || id != c.id || pass != c.pass {
			t.Fatalf("%q → id=%q pass=%q err=%v", c.in, id, pass, err)
		}
	}
}

func TestGetShareReturnsTheFilesAndPassToken(t *testing.T) {
	var gotPath, gotShare, gotPass string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotShare = r.URL.Query().Get("share_id")
		gotPass = r.URL.Query().Get("pass_code")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"share_status":    "OK",
			"pass_code_token": "tok-1",
			"share_id":        "VOabc123",
			"title":           "大模型资料",
			"files": []map[string]any{
				{"id": "sf1", "parent_id": "", "name": "资料", "kind": "drive#folder"},
				{"id": "sf2", "parent_id": "", "name": "notes.pdf", "kind": "drive#file", "size": "12"},
			},
			"next_page_token": "",
		})
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv.URL, &fakeTokens{token: "tok"})

	info, err := c.GetShare(context.Background(), "VOabc123", "7f3a")
	if err != nil {
		t.Fatalf("GetShare: %v", err)
	}
	if gotPath != "/drive/v1/share" || gotShare != "VOabc123" || gotPass != "7f3a" {
		t.Fatalf("request path=%q share=%q pass=%q", gotPath, gotShare, gotPass)
	}
	if info.Title != "大模型资料" || info.PassCodeToken != "tok-1" || len(info.Files) != 2 {
		t.Fatalf("info = %+v", info)
	}
	if info.Files[0].Name != "资料" || info.Files[0].Kind != "drive#folder" {
		t.Fatalf("files[0] = %+v", info.Files[0])
	}
}

func TestGetShareTellsAPassCodeApart(t *testing.T) {
	status := "PASS_CODE_EMPTY"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"share_status": status})
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv.URL, &fakeTokens{token: "tok"})

	if _, err := c.GetShare(context.Background(), "VOabc123", ""); !errors.Is(err, ErrPassCodeRequired) {
		t.Fatalf("empty pass: %v", err)
	}
	status = "PASS_CODE_ERROR"
	if _, err := c.GetShare(context.Background(), "VOabc123", "bad"); !errors.Is(err, ErrPassCodeWrong) {
		t.Fatalf("wrong pass: %v", err)
	}
}

func TestListShareFilesBrowsesAFolder(t *testing.T) {
	var gotPath, gotParent, gotToken, gotPage string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotParent = r.URL.Query().Get("parent_id")
		gotToken = r.URL.Query().Get("pass_code_token")
		gotPage = r.URL.Query().Get("page_token")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"files":           []map[string]any{{"id": "sf3", "parent_id": "sf1", "name": "inner.bin", "kind": "drive#file", "size": "3"}},
			"next_page_token": "next-1",
		})
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv.URL, &fakeTokens{token: "tok"})

	page, err := c.ListShareFiles(context.Background(), "VOabc123", "sf1", "tok-1", "p1")
	if err != nil {
		t.Fatalf("ListShareFiles: %v", err)
	}
	if gotPath != "/drive/v1/share/detail" || gotParent != "sf1" || gotToken != "tok-1" || gotPage != "p1" {
		t.Fatalf("request path=%q parent=%q token=%q page=%q", gotPath, gotParent, gotToken, gotPage)
	}
	if len(page.Items) != 1 || page.Items[0].Name != "inner.bin" || page.NextPageToken != "next-1" {
		t.Fatalf("page = %+v", page)
	}
}

func TestGetShareFileLinkReadsTheShareEndpoint(t *testing.T) {
	var gotPath, gotShare, gotFile, gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotShare = r.URL.Query().Get("share_id")
		gotFile = r.URL.Query().Get("file_id")
		gotToken = r.URL.Query().Get("pass_code_token")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"file_info": map[string]any{
				"id":               "sf2",
				"name":             "notes.pdf",
				"size":             "12",
				"web_content_link": "https://cdn.example/notes.pdf",
			},
		})
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv.URL, &fakeTokens{token: "tok"})

	link, err := c.GetShareFileLink(context.Background(), "VOabc123", "sf2", "tok-1")
	if err != nil {
		t.Fatalf("GetShareFileLink: %v", err)
	}
	if gotPath != "/drive/v1/share/file_info" || gotShare != "VOabc123" || gotFile != "sf2" || gotToken != "tok-1" {
		t.Fatalf("request path=%q share=%q file=%q token=%q", gotPath, gotShare, gotFile, gotToken)
	}
	if link.URL != "https://cdn.example/notes.pdf" || link.Size != 12 {
		t.Fatalf("link = %+v", link)
	}
}

func TestRestoreSharePostsTheSelectedFiles(t *testing.T) {
	var gotPath string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{"task_id": "t1"})
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv.URL, &fakeTokens{token: "tok"})

	if _, err := c.RestoreShare(context.Background(), "VOabc123", "tok-1", []string{"sf2", "sf3"}); err != nil {
		t.Fatalf("RestoreShare: %v", err)
	}
	if gotPath != "/drive/v1/share/restore" {
		t.Fatalf("path = %q", gotPath)
	}
	if body["share_id"] != "VOabc123" || body["pass_code_token"] != "tok-1" {
		t.Fatalf("body = %v", body)
	}
	ids, _ := body["file_ids"].([]any)
	if len(ids) != 2 || ids[0] != "sf2" {
		t.Fatalf("file_ids = %v", body["file_ids"])
	}
}

func TestShareReportsItsCreator(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
		want string
	}{
		{"share_creator name", map[string]any{"share_creator": map[string]any{"name": "张三"}}, "张三"},
		{"creator name", map[string]any{"creator": map[string]any{"name": "李四"}}, "李四"},
		{"share_creator user_name", map[string]any{"share_creator": map[string]any{"user_name": "wangwu"}}, "wangwu"},
		{"missing", map[string]any{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tc.body["share_status"] = "OK"
				tc.body["pass_code_token"] = "tok-1"
				_ = json.NewEncoder(w).Encode(tc.body)
			}))
			t.Cleanup(srv.Close)
			c := newTestClient(t, srv.URL, &fakeTokens{token: "tok"})

			info, err := c.GetShare(context.Background(), "VOabc123", "")
			if err != nil {
				t.Fatalf("GetShare: %v", err)
			}
			if info.Owner != tc.want {
				t.Fatalf("owner = %q, want %q", info.Owner, tc.want)
			}
		})
	}
}

func TestRestoreShareReturnsTheTaskID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"task_id": "t1"})
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv.URL, &fakeTokens{token: "tok"})

	taskID, err := c.RestoreShare(context.Background(), "VOabc123", "tok-1", []string{"sf2"})
	if err != nil {
		t.Fatalf("RestoreShare: %v", err)
	}
	if taskID != "t1" {
		t.Fatalf("task id = %q", taskID)
	}
}
