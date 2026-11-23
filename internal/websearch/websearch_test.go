package websearch

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// organicServer answers every search with the organic results it was built
// with and records the query, key and page it saw.
func organicServer(t *testing.T, results []map[string]any) (*httptest.Server, *string, *string, *int) {
	t.Helper()
	var gotQuery, gotKey string
	gotPage := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		var body struct {
			Q    string `json:"q"`
			Page int    `json:"page"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		// The first query is the one these tests are about; the page is filled
		// from the following hosts when the first one answers with little.
		if gotQuery == "" {
			gotQuery = body.Q
			gotKey = r.Header.Get("X-API-KEY")
			gotPage = body.Page
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"organic": results})
	}))
	t.Cleanup(srv.Close)
	return srv, &gotQuery, &gotKey, &gotPage
}

func TestSearchAddsTheSiteFilterAndReturnsResults(t *testing.T) {
	srv, gotQuery, gotKey, _ := organicServer(t, []map[string]any{
		{"title": "大模型合集", "link": "https://mypikpak.com/s/VN1", "snippet": "分享的大模型资料"},
		{"title": "第二份", "link": "https://www.mypikpak.com/s/VN2", "snippet": "更多资料"},
	})
	c := &Client{Endpoint: srv.URL, HTTP: srv.Client()}

	ans, err := c.Search(context.Background(), "key-123", "  大模型 ", 1, ScopeShare)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	got := ans.Results
	// The site filter is the server's business: the person types only the
	// keyword, and a pasted "site:" prefix never doubles up.
	if *gotQuery != "site:mypikpak.com 大模型" {
		t.Fatalf("query = %q", *gotQuery)
	}
	if *gotKey != "key-123" {
		t.Fatalf("key header = %q", *gotKey)
	}
	if len(got) != 2 {
		t.Fatalf("results = %+v", got)
	}
	if got[0].Title != "大模型合集" || got[0].URL != "https://mypikpak.com/s/VN1" || got[0].Snippet != "分享的大模型资料" {
		t.Fatalf("result[0] = %+v", got[0])
	}
}

func TestSearchStripsASiteFilterThePersonPasted(t *testing.T) {
	srv, gotQuery, _, _ := organicServer(t, []map[string]any{
		{"title": "主站", "link": "https://mypikpak.com/s/VN1", "snippet": "相关"},
	})
	c := &Client{Endpoint: srv.URL, HTTP: srv.Client()}

	if _, err := c.Search(context.Background(), "key-123", "site://mypikpak.com/ 大模型", 1, ScopeShare); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if *gotQuery != "site:mypikpak.com 大模型" {
		t.Fatalf("query = %q", *gotQuery)
	}
}

func TestSearchAsksForTheRequestedPage(t *testing.T) {
	srv, _, _, gotPage := organicServer(t, nil)
	c := &Client{Endpoint: srv.URL, HTTP: srv.Client()}

	if _, err := c.Search(context.Background(), "key-123", "大模型", 3, ScopeShare); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if *gotPage != 3 {
		t.Fatalf("page = %d", *gotPage)
	}
}

func TestSearchKeepsEveryPikPakShareHost(t *testing.T) {
	srv, _, _, _ := organicServer(t, []map[string]any{
		{"title": "知乎", "link": "https://www.zhihu.com/question/1", "snippet": "无关"},
		{"title": "主站", "link": "https://mypikpak.com/s/VN1", "snippet": "相关"},
		// PikPak shares appear on its sister domains too; dropping them made a
		// page of ten look like three.
		{"title": "备用域名", "link": "https://mypikpak.net/s/VN2", "snippet": "相关"},
		{"title": "国际站", "link": "https://pikpak.me/s/VN3", "snippet": "相关"},
		// A lookalike host must not pass for the site.
		{"title": "假冒", "link": "https://notmypikpak.com/s/VN1", "snippet": "相关"},
	})
	c := &Client{Endpoint: srv.URL, HTTP: srv.Client()}

	ans, err := c.Search(context.Background(), "key-123", "大模型", 1, ScopeShare)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	got := ans.Results
	urls := make([]string, 0, len(got))
	for _, r := range got {
		urls = append(urls, r.URL)
	}
	want := []string{"https://mypikpak.com/s/VN1", "https://mypikpak.net/s/VN2", "https://pikpak.me/s/VN3"}
	if len(urls) != len(want) {
		t.Fatalf("results = %v", urls)
	}
	for i := range want {
		if urls[i] != want[i] {
			t.Fatalf("results = %v", urls)
		}
	}
}

// A site: query keeps the results on the domain, not on share pages: PikPak's
// own homepage, help and download pages pass the operator too, and none of
// them can be opened in the share browser.
func TestSearchDropsPagesOnTheSiteThatAreNotShares(t *testing.T) {
	srv, _ := siteAnswers(t, map[string][]map[string]any{
		"mypikpak.com": {
			{"title": "首页", "link": "https://mypikpak.com/", "snippet": "无关"},
			{"title": "帮助", "link": "https://mypikpak.com/help/contact", "snippet": "无关"},
			{"title": "分享", "link": "https://mypikpak.com/s/VN1", "snippet": "相关"},
		},
	})
	c := &Client{Endpoint: srv.URL, HTTP: srv.Client()}

	ans, err := c.Search(context.Background(), "key-123", "大模型", 1, ScopeShare)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(ans.Results) != 1 || ans.Results[0].URL != "https://mypikpak.com/s/VN1" {
		t.Fatalf("results = %+v", ans.Results)
	}
	if ans.Raw != 3 {
		t.Fatalf("raw = %d, want the three organic hits", ans.Raw)
	}
}

// When the scope allows it, every hit the provider returned is kept — the
// provider does not always honor the site: operator — and the shares are
// marked apart from the rest.
func TestSearchKeepsEveryHitWhenTheScopeAllowsIt(t *testing.T) {
	srv, _ := siteAnswers(t, map[string][]map[string]any{
		"mypikpak.com": {
			{"title": "首页", "link": "https://mypikpak.com/", "snippet": "站内页面"},
			{"title": "分享", "link": "https://mypikpak.com/s/VN1", "snippet": "相关"},
			{"title": "站外", "link": "https://example.com/x", "snippet": "无关"},
		},
	})
	c := &Client{Endpoint: srv.URL, HTTP: srv.Client()}

	ans, err := c.Search(context.Background(), "key-123", "大模型", 1, ScopeSite)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(ans.Results) != 3 {
		t.Fatalf("results = %+v", ans.Results)
	}
	if ans.Results[0].Share || ans.Results[0].URL != "https://mypikpak.com/" {
		t.Fatalf("first result = %+v", ans.Results[0])
	}
	if !ans.Results[1].Share || ans.Results[1].URL != "https://mypikpak.com/s/VN1" {
		t.Fatalf("second result = %+v", ans.Results[1])
	}
	if ans.Results[2].Share || ans.Results[2].URL != "https://example.com/x" {
		t.Fatalf("third result = %+v", ans.Results[2])
	}
}

// Google sometimes answers with its own click redirect instead of the page
// address. A wrapped share is still a share: it has to be unwrapped before
// the list decides what it is and whether it can be opened.
func TestSearchUnwrapsAProviderRedirect(t *testing.T) {
	dest := "https://mypikpak.com/s/VN9"
	// /goto carries a protobuf whose string field holds the address.
	payload := append([]byte{0x08, 0x01, 0x12, byte(len(dest))}, []byte(dest)...)
	wrapped := "https://www.google.com/goto?url=" + base64.RawURLEncoding.EncodeToString(payload)
	srv, _ := siteAnswers(t, map[string][]map[string]any{
		"mypikpak.com": {
			{"title": "分享", "link": wrapped, "snippet": "相关"},
		},
	})
	c := &Client{Endpoint: srv.URL, HTTP: srv.Client()}

	ans, err := c.Search(context.Background(), "key-123", "大模型", 1, ScopeShare)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(ans.Results) != 1 {
		t.Fatalf("results = %+v", ans.Results)
	}
	if ans.Results[0].URL != dest || !ans.Results[0].Share {
		t.Fatalf("result = %+v", ans.Results[0])
	}
}

// Google's newer /goto token is encrypted and cannot be decoded offline.
// Searching no longer asks the provider to resolve it — a page can hold
// twenty of them and each costs a round trip — so the hit is listed as a
// wrapper, marked for resolution when it is opened, and kept even under the
// share scope: most of them lead to a share.
func TestSearchListsAGotoWrapperWithoutResolvingIt(t *testing.T) {
	wrapped := "https://www.google.com/goto?url=CAESencryptedtoken"
	dest := "https://mypikpak.com/s/VN9"
	srv, asked := providerServer(t, []map[string]any{
		{"title": "分享", "link": wrapped, "snippet": "相关", "displayedLink": "mypikpak.com/s/VN9"},
	}, map[string]string{wrapped: dest})
	c := &Client{Endpoint: srv.URL + "/search", ResolveEndpoint: srv.URL + "/resolve", HTTP: srv.Client()}

	ans, err := c.Search(context.Background(), "key-123", "大模型", 1, ScopeShare)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(ans.Results) != 1 {
		t.Fatalf("results = %+v", ans.Results)
	}
	got := ans.Results[0]
	if got.URL != wrapped || !got.Wrapped || got.Share {
		t.Fatalf("result = %+v", got)
	}
	if got.Display != "mypikpak.com/s/VN9" {
		t.Fatalf("display = %q", got.Display)
	}
	if len(*asked) != 0 {
		t.Fatalf("the search resolved %v", *asked)
	}
}

// The same share wrapped behind the provider's redirect reaches the list
// from more than one host, and each host's token is different. What the
// provider showed under the title is the same, so that is what it is deduped
// by.
func TestSearchShowsAWrappedShareOnceAcrossHosts(t *testing.T) {
	srv, _ := siteAnswers(t, map[string][]map[string]any{
		"mypikpak.com": {
			{"title": "分享", "link": "https://www.google.com/goto?url=CAESa", "snippet": "相关", "displayedLink": "mypikpak.com/s/VN9"},
		},
		"mypikpak.net": {
			{"title": "分享", "link": "https://www.google.com/goto?url=CAESb", "snippet": "相关", "displayedLink": "mypikpak.com/s/VN9"},
		},
	})
	c := &Client{Endpoint: srv.URL, HTTP: &http.Client{Transport: offlineGoogle{next: srv.Client().Transport}}}

	ans, err := c.Search(context.Background(), "key-123", "大模型", 1, ScopeShare)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(ans.Results) != 1 || !ans.Results[0].Wrapped {
		t.Fatalf("results = %+v", ans.Results)
	}
}

// The provider's free /resolve endpoint knows a wrapper's destination.
func TestResolveLinkAsksTheProvider(t *testing.T) {
	wrapped := "https://www.google.com/goto?url=CAESencryptedtoken"
	dest := "https://mypikpak.com/s/VN9"
	srv, asked := providerServer(t, nil, map[string]string{wrapped: dest})
	c := &Client{
		ResolveEndpoint: srv.URL + "/resolve",
		HTTP:            &http.Client{Transport: offlineGoogle{next: srv.Client().Transport}},
	}

	got, err := c.ResolveLink(context.Background(), "key-123", wrapped)
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if got != dest {
		t.Fatalf("destination = %q", got)
	}
	if len(*asked) != 1 || (*asked)[0] != wrapped {
		t.Fatalf("resolved links = %v", *asked)
	}
}

// When the provider leaves the wrapper alone, Google's own redirect still
// names the destination; a real /goto answers 302 and nothing else has to be
// fetched.
func TestResolveLinkReadsGoogleRedirectWhenTheProviderFails(t *testing.T) {
	wrapped := "https://www.google.com/goto?url=CAESencryptedtoken"
	dest := "https://mypikpak.com/s/VN9"
	srv, _ := providerServer(t, nil, nil)
	c := &Client{
		ResolveEndpoint: srv.URL + "/resolve",
		HTTP: &http.Client{Transport: redirectTransport{
			next: srv.Client().Transport, from: "www.google.com", to: dest,
		}},
	}

	got, err := c.ResolveLink(context.Background(), "key-123", wrapped)
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if got != dest {
		t.Fatalf("destination = %q", got)
	}
}

// A /url redirect names its destination in the query, so it never needs the
// network.
func TestResolveLinkDecodesTheOlderRedirectLocally(t *testing.T) {
	c := &Client{HTTP: &http.Client{Transport: offlineGoogle{next: http.DefaultTransport}}}

	got, err := c.ResolveLink(context.Background(), "key-123", "https://www.google.com/url?q=https%3A%2F%2Fmypikpak.com%2Fs%2FVN1&sa=U")
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if got != "https://mypikpak.com/s/VN1" {
		t.Fatalf("destination = %q", got)
	}
}

// An address that is not a provider redirect is already the destination; the
// caller opens it as it is.
func TestResolveLinkLeavesAPlainAddressAlone(t *testing.T) {
	c := &Client{HTTP: &http.Client{Transport: offlineGoogle{next: http.DefaultTransport}}}

	if _, err := c.ResolveLink(context.Background(), "key-123", "https://mypikpak.com/s/VN9"); !errors.Is(err, ErrNotWrapped) {
		t.Fatalf("err = %v", err)
	}
}

// Both ways can fail, and neither may hang the click: with the provider
// refusing and Google unreachable the answer is a quick, explicit failure so
// the page can fall back to the wrapper itself.
func TestResolveLinkReportsWhenNothingAnswers(t *testing.T) {
	wrapped := "https://www.google.com/goto?url=CAESencryptedtoken"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	c := &Client{
		ResolveEndpoint: srv.URL + "/resolve",
		HTTP:            &http.Client{Transport: offlineGoogle{next: srv.Client().Transport}},
	}

	if _, err := c.ResolveLink(context.Background(), "key-123", wrapped); !errors.Is(err, ErrLinkUnresolved) {
		t.Fatalf("err = %v", err)
	}
}

// redirectTransport answers a request to one host with a redirect and passes
// everything else through, the way Google's click redirect does.
type redirectTransport struct {
	next http.RoundTripper
	from string
	to   string
}

func (t redirectTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if strings.EqualFold(r.URL.Hostname(), t.from) {
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{t.to}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    r,
		}, nil
	}
	return t.next.RoundTrip(r)
}

// offlineGoogle fails every request to a Google host, so a test that leaves a
// wrapper unresolved does not reach the network.
type offlineGoogle struct{ next http.RoundTripper }

func (t offlineGoogle) RoundTrip(r *http.Request) (*http.Response, error) {
	if strings.HasSuffix(strings.ToLower(r.URL.Hostname()), "google.com") {
		return nil, errors.New("google is unreachable in tests")
	}
	return t.next.RoundTrip(r)
}

// providerServer answers searches with organic results and resolves wrapped
// links through a second path on the same server, recording what it resolved.
func providerServer(t *testing.T, organic []map[string]any, dests map[string]string) (*httptest.Server, *[]string) {
	t.Helper()
	asked := &[]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/resolve") {
			var body struct {
				Organic []struct {
					Link string `json:"link"`
				} `json:"organic"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			out := make([]map[string]any, 0, len(body.Organic))
			for _, item := range body.Organic {
				*asked = append(*asked, item.Link)
				dest := dests[item.Link]
				if dest == "" {
					dest = item.Link
				}
				out = append(out, map[string]any{"link": dest})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"organic": out, "credits": 0})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"organic": organic})
	}))
	t.Cleanup(srv.Close)
	return srv, asked
}

// A provider result is untrusted text, so only Google's own domains may be
// treated as its click redirects; a look-alike host would otherwise make the
// backend fetch an address the result chose.
func TestIsGoogleHostRejectsLookAlikes(t *testing.T) {
	cases := map[string]bool{
		"google.com":          true,
		"www.google.com":      true,
		"google.com.hk":       true,
		"www.google.co.uk":    true,
		"google.de":           true,
		"google.evil.com":     false,
		"google.com.evil.com": false,
		"notgoogle.com":       false,
	}
	for host, want := range cases {
		if got := isGoogleHost(host); got != want {
			t.Fatalf("isGoogleHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestUnwrapLink(t *testing.T) {
	dest := "https://mypikpak.com/s/VN9"
	payload := append([]byte{0x08, 0x01, 0x12, byte(len(dest))}, []byte(dest)...)
	cases := []struct{ in, want string }{
		// The older /url redirect names the address in a query parameter.
		{"https://www.google.com/url?q=https%3A%2F%2Fmypikpak.com%2Fs%2FVN1&sa=U", "https://mypikpak.com/s/VN1"},
		{"https://www.google.com/goto?url=" + base64.RawURLEncoding.EncodeToString(payload), dest},
		// A plain address is already the destination.
		{dest, dest},
		{"https://www.zhihu.com/question/1", "https://www.zhihu.com/question/1"},
		// A google page that is not a redirect stays as it is.
		{"https://www.google.com/search?q=%E5%A4%A7%E6%A8%A1%E5%9E%8B", "https://www.google.com/search?q=%E5%A4%A7%E6%A8%A1%E5%9E%8B"},
	}
	for _, c := range cases {
		if got := unwrapLink(c.in); got != c.want {
			t.Fatalf("unwrapLink(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSearchRefusesAnEmptyKeywordWithoutCallingUpstream(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
	}))
	t.Cleanup(srv.Close)
	c := &Client{Endpoint: srv.URL, HTTP: srv.Client()}

	if _, err := c.Search(context.Background(), "key-123", "  ", 1, ScopeShare); !errors.Is(err, ErrEmptyQuery) {
		t.Fatalf("err = %v", err)
	}
	if calls != 0 {
		t.Fatalf("upstream calls = %d", calls)
	}
}

func TestSearchReportsAnUpstreamFailureWithoutLeakingTheKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)
	c := &Client{Endpoint: srv.URL, HTTP: srv.Client()}

	_, err := c.Search(context.Background(), "key-123", "大模型", 1, ScopeShare)
	if err == nil {
		t.Fatal("a rejected request looked like an empty result")
	}
	if strings.Contains(err.Error(), "key-123") {
		t.Fatalf("the API key reached the error text: %v", err)
	}
}

// One site is searched at a time: the next host is asked only after the
// previous one answered, and every host is asked while the page is short.
func TestSearchAsksTheSitesOneAtATime(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Q string `json:"q"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		queries = append(queries, body.Q)
		if strings.Contains(body.Q, "site:mypikpak.net") {
			_ = json.NewEncoder(w).Encode(map[string]any{"organic": []map[string]any{
				{"title": "备用域名", "link": "https://mypikpak.net/s/VN2", "snippet": "相关"},
			}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"organic": []map[string]any{}})
	}))
	t.Cleanup(srv.Close)
	c := &Client{Endpoint: srv.URL, HTTP: srv.Client()}

	ans, err := c.Search(context.Background(), "key-123", "大模型", 1, ScopeShare)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	got := ans.Results
	if len(queries) != len(shareSites) {
		t.Fatalf("queries = %v", queries)
	}
	for i, site := range shareSites {
		if !strings.Contains(queries[i], "site:"+site+" ") {
			t.Fatalf("queries = %v", queries)
		}
	}
	if len(got) != 1 || got[0].URL != "https://mypikpak.net/s/VN2" {
		t.Fatalf("results = %+v", got)
	}
}

// siteAnswers serves one organic page per site keyword and records every
// query, so a test can see which hosts were asked and in what order.
func siteAnswers(t *testing.T, pages map[string][]map[string]any) (*httptest.Server, *[]string) {
	t.Helper()
	queries := &[]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Q string `json:"q"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		*queries = append(*queries, body.Q)
		results := []map[string]any{}
		for site, page := range pages {
			if strings.Contains(body.Q, "site:"+site+" ") {
				results = page
				break
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"organic": results})
	}))
	t.Cleanup(srv.Close)
	return srv, queries
}

// The raw count is what the provider returned before the shares were picked
// out of it: a page that drops most of its hits says so.
func TestSearchReportsHowManyRawHitsTheProviderReturned(t *testing.T) {
	srv, _ := siteAnswers(t, map[string][]map[string]any{
		"mypikpak.com": {
			{"title": "主站", "link": "https://mypikpak.com/s/VN1", "snippet": "相关"},
			{"title": "论坛", "link": "https://www.zhihu.com/question/1", "snippet": "无关"},
			{"title": "博客", "link": "https://blog.example.com/p/1", "snippet": "无关"},
		},
		"mypikpak.net": {
			{"title": "备用域名", "link": "https://mypikpak.net/s/VN2", "snippet": "相关"},
		},
	})
	c := &Client{Endpoint: srv.URL, HTTP: srv.Client()}

	ans, err := c.Search(context.Background(), "key-123", "大模型", 1, ScopeShare)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(ans.Results) != 2 {
		t.Fatalf("results = %+v", ans.Results)
	}
	if ans.Raw != 4 {
		t.Fatalf("raw = %d, want 4 (3 from the first host, 1 from the second)", ans.Raw)
	}
}

// The same list from two hosts is one set of hits: raw counts each link once,
// otherwise a provider that ignores site: would inflate the number fourfold.
func TestSearchCountsALinkOnceAcrossHosts(t *testing.T) {
	same := []map[string]any{
		{"title": "论坛", "link": "https://www.zhihu.com/question/1", "snippet": "无关"},
		{"title": "主站", "link": "https://mypikpak.com/s/VN1", "snippet": "相关"},
	}
	srv, _ := siteAnswers(t, map[string][]map[string]any{
		"mypikpak.com": same,
		"mypikpak.net": same,
	})
	c := &Client{Endpoint: srv.URL, HTTP: srv.Client()}

	ans, err := c.Search(context.Background(), "key-123", "大模型", 1, ScopeShare)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(ans.Results) != 1 {
		t.Fatalf("results = %+v", ans.Results)
	}
	if ans.Raw != 2 {
		t.Fatalf("raw = %d, want 2 (each link counted once)", ans.Raw)
	}
}

// A sparse page is filled from the hosts after it: Google's index for one
// share domain often holds only a few hits for a keyword, and a page of
// three is not a page.
func TestSearchFillsThePageFromTheNextSitesWhenTheFirstIsSparse(t *testing.T) {
	srv, queries := siteAnswers(t, map[string][]map[string]any{
		"mypikpak.com": {
			{"title": "主站", "link": "https://mypikpak.com/s/VN1", "snippet": "相关"},
			{"title": "论坛", "link": "https://www.zhihu.com/question/1", "snippet": "无关"},
		},
		"mypikpak.net": {
			{"title": "备用域名", "link": "https://mypikpak.net/s/VN2", "snippet": "相关"},
		},
	})
	c := &Client{Endpoint: srv.URL, HTTP: srv.Client()}

	ans, err := c.Search(context.Background(), "key-123", "大模型", 1, ScopeShare)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	got := ans.Results
	if len(got) != 2 || got[0].URL != "https://mypikpak.com/s/VN1" || got[1].URL != "https://mypikpak.net/s/VN2" {
		t.Fatalf("results = %+v", got)
	}
	if len(*queries) != len(shareSites) {
		t.Fatalf("queries = %v", *queries)
	}
	for i, site := range shareSites {
		if !strings.Contains((*queries)[i], "site:"+site+" ") {
			t.Fatalf("queries = %v", *queries)
		}
	}
}

// Once the page is full there is nothing to add: the remaining hosts cost a
// paid query each and would only repeat what the first one answered.
func TestSearchStopsAskingSitesOnceThePageIsFull(t *testing.T) {
	full := make([]map[string]any, 0, pageSize)
	for i := 0; i < pageSize; i++ {
		full = append(full, map[string]any{
			"title":   "结果",
			"link":    "https://mypikpak.com/s/VN" + strconv.Itoa(i),
			"snippet": "相关",
		})
	}
	srv, queries := siteAnswers(t, map[string][]map[string]any{"mypikpak.com": full})
	c := &Client{Endpoint: srv.URL, HTTP: srv.Client()}

	ans, err := c.Search(context.Background(), "key-123", "大模型", 1, ScopeShare)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	got := ans.Results
	if len(got) != pageSize {
		t.Fatalf("results = %d", len(got))
	}
	if len(*queries) != 1 {
		t.Fatalf("queries = %v", *queries)
	}
}

// The same share is served by every PikPak host under the same share id; a
// page filled from two hosts must not show it twice.
func TestSearchShowsAShareOnceAcrossHosts(t *testing.T) {
	srv, _ := siteAnswers(t, map[string][]map[string]any{
		"mypikpak.com": {{"title": "主站", "link": "https://mypikpak.com/s/VNSAME", "snippet": "相关"}},
		"mypikpak.net": {{"title": "备用域名", "link": "https://mypikpak.net/s/VNSAME", "snippet": "相关"}},
	})
	c := &Client{Endpoint: srv.URL, HTTP: srv.Client()}

	ans, err := c.Search(context.Background(), "key-123", "大模型", 1, ScopeShare)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	got := ans.Results
	if len(got) != 1 || got[0].URL != "https://mypikpak.com/s/VNSAME" {
		t.Fatalf("results = %+v", got)
	}
}

// A host that cannot be reached is skipped: mypikpak.me no longer resolves,
// and one dead host must not turn a search into a failure.
func TestSearchSkipsAHostThatCannotBeReached(t *testing.T) {
	srv, queries := siteAnswers(t, map[string][]map[string]any{
		"mypikpak.com": {{"title": "主站", "link": "https://mypikpak.com/s/VN1", "snippet": "相关"}},
		"pikpak.me":    {{"title": "国际站", "link": "https://pikpak.me/s/VN3", "snippet": "相关"}},
	})
	c := &Client{Endpoint: srv.URL, HTTP: &http.Client{
		Transport: failingTransport{next: srv.Client().Transport, failOn: "site:mypikpak.net "},
	}}

	ans, err := c.Search(context.Background(), "key-123", "大模型", 1, ScopeShare)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	got := ans.Results
	if len(got) != 2 || got[1].URL != "https://pikpak.me/s/VN3" {
		t.Fatalf("results = %+v", got)
	}
	if len(*queries) != 3 {
		t.Fatalf("queries = %v", *queries)
	}
}

// failingTransport refuses the requests whose body carries failOn, the way a
// host that stopped resolving does.
type failingTransport struct {
	next   http.RoundTripper
	failOn string
}

func (t failingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Body != nil {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		if strings.Contains(string(body), t.failOn) {
			return nil, &net.DNSError{Err: "no such host", Name: "mypikpak.net", IsNotFound: true}
		}
	}
	return t.next.RoundTrip(r)
}

// A provider refusal is not a dead host: a rejected key must surface, not be
// hidden behind three more failing queries.
func TestSearchReportsAProviderRefusalWithoutAskingTheOtherSites(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	c := &Client{Endpoint: srv.URL, HTTP: srv.Client()}

	_, err := c.Search(context.Background(), "key-123", "大模型", 1, ScopeShare)
	var upstream *UpstreamError
	if !errors.As(err, &upstream) || upstream.Status != http.StatusForbidden {
		t.Fatalf("err = %v", err)
	}
	if calls != 1 {
		t.Fatalf("upstream calls = %d", calls)
	}
}
