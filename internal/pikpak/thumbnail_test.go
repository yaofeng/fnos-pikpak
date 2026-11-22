package pikpak

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListFilesAsksForThumbnailsAndParsesThem(t *testing.T) {
	var gotSize string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSize = r.URL.Query().Get("thumbnail_size")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"files": []map[string]any{{
				"id": "f1", "name": "a.jpg", "kind": "drive#file", "size": "9",
				"thumbnail_link": "https://img.example/x.jpg",
			}},
		})
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv.URL, &fakeTokens{token: "tok"})

	page, err := c.ListFiles(context.Background(), "", ListOptions{Limit: 100})
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if gotSize != "SIZE_LARGE" {
		t.Fatalf("thumbnail_size = %q", gotSize)
	}
	if len(page.Items) != 1 || page.Items[0].ThumbnailLink != "https://img.example/x.jpg" {
		t.Fatalf("items = %+v", page.Items)
	}
}

func TestShareFileLinkCarriesItsThumbnail(t *testing.T) {
	var gotSize string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSize = r.URL.Query().Get("thumbnail_size")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"file_info": map[string]any{
				"id": "sf2", "name": "notes.mp4", "size": "9",
				"web_content_link": "https://cdn.example/notes.mp4",
				"thumbnail_link":   "https://img.example/sf2.jpg",
			},
		})
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv.URL, &fakeTokens{token: "tok"})

	link, err := c.GetShareFileLink(context.Background(), "VOabc123", "sf2", "tok-1")
	if err != nil {
		t.Fatalf("GetShareFileLink: %v", err)
	}
	if gotSize != "SIZE_LARGE" {
		t.Fatalf("thumbnail_size = %q", gotSize)
	}
	if link.ThumbnailLink != "https://img.example/sf2.jpg" {
		t.Fatalf("thumbnail = %q", link.ThumbnailLink)
	}
}
