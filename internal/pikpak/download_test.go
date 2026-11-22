package pikpak

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetDownloadLinkPrefersWebContentLink(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/drive/v1/files/f1" {
			t.Errorf("path = %q", r.URL.Path)
		}
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{
			"id":"f1","size":"1234",
			"web_content_link":"https://dl.example/a?expire=1",
			"links":{"application/octet-stream":{"url":"https://dl.example/b"}}
		}`)
	}))
	defer srv.Close()

	link, err := newTestClient(t, srv.URL, &fakeTokens{token: "t"}).GetDownloadLink(context.Background(), "f1")
	if err != nil {
		t.Fatalf("GetDownloadLink: %v", err)
	}
	if link.URL != "https://dl.example/a?expire=1" {
		t.Fatalf("url = %q", link.URL)
	}
	if link.Size != 1234 {
		t.Fatalf("size = %d", link.Size)
	}
	if gotQuery != "usage=FETCH&_magic=2021&thumbnail_size=SIZE_LARGE" {
		t.Fatalf("query = %q", gotQuery)
	}
}

func TestGetDownloadLinkFallsBackToLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"f1","size":"7","links":{"application/octet-stream":{"url":"https://dl.example/b"}}}`)
	}))
	defer srv.Close()

	link, err := newTestClient(t, srv.URL, &fakeTokens{token: "t"}).GetDownloadLink(context.Background(), "f1")
	if err != nil || link.URL != "https://dl.example/b" {
		t.Fatalf("link=%+v err=%v", link, err)
	}
}

func TestGetDownloadLinkWithoutAnyURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"f1","size":"7","links":{}}`)
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv.URL, &fakeTokens{token: "t"}).GetDownloadLink(context.Background(), "f1")
	if !errors.Is(err, ErrNoDownloadLink) {
		t.Fatalf("err = %v, want ErrNoDownloadLink", err)
	}
}
