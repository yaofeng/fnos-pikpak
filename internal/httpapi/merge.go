package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"sync"

	"fnos-pikpak/internal/pikpak"
	"fnos-pikpak/internal/probe"
)

// accountError describes one account's share of a merged listing that failed,
// so a dead credential cannot silently blank that account's rows. The error
// text follows the per-account shape of GET /api/accounts.
type accountError struct {
	AccountID   string `json:"account_id"`
	AccountName string `json:"account_name"`
	Error       string `json:"error"`
}

// recentResponse is the merged 近期 listing: one page per account, ordered by
// modified time. The token is the handler's own envelope and carries one
// upstream token per account that still has pages.
type recentResponse struct {
	Items         []pikpak.File  `json:"items"`
	NextPageToken string         `json:"next_page_token,omitempty"`
	AccountErrors []accountError `json:"account_errors,omitempty"`
}

// tasksResponse is the merged task listing: accounts concatenated in bind
// order, filters applied to the merged slice.
type tasksResponse struct {
	Items         []pikpak.Task  `json:"items"`
	NextPageToken string         `json:"next_page_token,omitempty"`
	AccountErrors []accountError `json:"account_errors,omitempty"`
}

// encodePageToken packs per-account upstream page tokens into one opaque
// string the browser can echo back. Empty input encodes to "", which lets the
// response omit the field once every account is exhausted.
func encodePageToken(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// decodePageToken splits a page token back into per-account upstream tokens.
// The empty string is the first page for every account; anything unreadable
// is a client error, not a silent restart from page one.
func decodePageToken(s string) (map[string]string, error) {
	if s == "" {
		return map[string]string{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]string{}
	}
	return m, nil
}

// stampFiles tags one account's rows with the account that served them, so
// the merged listing can drive per-account actions. A legacy credential-only
// account has no id and stays unstamped rather than carrying an empty tag.
func stampFiles(items []pikpak.File, acct namedClient) []pikpak.File {
	if acct.Account.ID == "" {
		return items
	}
	for i := range items {
		items[i].AccountID = acct.Account.ID
	}
	return items
}

// stampTasks is stampFiles for task rows, which also carry the display name
// so the interface can show who owns each task.
func stampTasks(items []pikpak.Task, acct namedClient) []pikpak.Task {
	if acct.Account.ID == "" {
		return items
	}
	for i := range items {
		items[i].AccountID = acct.Account.ID
		items[i].AccountName = acct.Account.DisplayName
	}
	return items
}

// mergeFilePages concatenates one page per account and orders the rows by
// modified time, newest first, stable within a timestamp. Rows whose
// timestamp cannot be parsed sort first — the place modifiedSortKey keeps
// them in the oldest-first clean-up walks. Every row is stamped with the
// account that served it.
func mergeFilePages(pages [][]pikpak.File, srcs []namedClient) []pikpak.File {
	var out []pikpak.File
	for i, page := range pages {
		if i >= len(srcs) {
			break
		}
		out = append(out, stampFiles(page, srcs[i])...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ki, kj := modifiedSortKey(out[i]), modifiedSortKey(out[j])
		if ki == "" {
			return kj != ""
		}
		if kj == "" {
			return false
		}
		return ki > kj
	})
	return out
}

// isAccountLevelErr reports whether a CreateOffline failure is the account's
// own fault — its daily quota (code 11), its rate limit, an upstream outage,
// a dead network path, or this NAS running out of space to receive the file
// — so rotating to the next account can help. Share-level refusals travel
// with the link and would fail on every account the same way, which is why
// they stop the rotation instead.
func isAccountLevelErr(err error) bool {
	if errors.Is(err, errSpaceInsufficient) {
		return true
	}
	var apiErr *pikpak.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code == 11 ||
			apiErr.Status == http.StatusTooManyRequests ||
			apiErr.Status >= 500
	}
	// A transport failure says nothing about the link: the next account may
	// simply have a working connection.
	var urlErr *url.Error
	return errors.As(err, &urlErr)
}

// fetchFilePages pulls one events page per account in parallel. A failed
// account leaves an empty page and a failure entry; it must not keep the
// other accounts from loading.
func fetchFilePages(ctx context.Context, clients []namedClient, tokens map[string]string) (pages [][]pikpak.File, nexts []string, failures []error) {
	pages = make([][]pikpak.File, len(clients))
	nexts = make([]string, len(clients))
	failures = make([]error, len(clients))
	var wg sync.WaitGroup
	for i, nc := range clients {
		wg.Add(1)
		go func(i int, nc namedClient) {
			defer wg.Done()
			page, err := nc.C.Events(ctx, tokens[nc.Account.ID])
			if err != nil {
				failures[i] = err
				return
			}
			pages[i] = page.Items
			nexts[i] = page.NextPageToken
		}(i, nc)
	}
	wg.Wait()
	return pages, nexts, failures
}

// fetchTaskPages is fetchFilePages for offline-task pages.
func fetchTaskPages(ctx context.Context, clients []namedClient, tokens map[string]string) (pages [][]pikpak.Task, nexts []string, failures []error) {
	pages = make([][]pikpak.Task, len(clients))
	nexts = make([]string, len(clients))
	failures = make([]error, len(clients))
	var wg sync.WaitGroup
	for i, nc := range clients {
		wg.Add(1)
		go func(i int, nc namedClient) {
			defer wg.Done()
			page, err := nc.C.ListTasks(ctx, tokens[nc.Account.ID])
			if err != nil {
				failures[i] = err
				return
			}
			pages[i] = page.Items
			nexts[i] = page.NextPageToken
		}(i, nc)
	}
	wg.Wait()
	return pages, nexts, failures
}

// nextPageToken re-packs the accounts that still have pages. Exhausted and
// failed accounts drop out; an empty result omits the field, ending the
// merged listing.
func nextPageToken(clients []namedClient, nexts []string, failures []error) string {
	m := map[string]string{}
	for i, nc := range clients {
		if failures[i] != nil || nexts[i] == "" {
			continue
		}
		m[nc.Account.ID] = nexts[i]
	}
	return encodePageToken(m)
}

// accountErrorList describes the accounts whose page could not be loaded,
// with the upstream message redacted the way GET /api/accounts does.
func accountErrorList(clients []namedClient, failures []error) []accountError {
	var out []accountError
	for i, nc := range clients {
		if failures[i] == nil {
			continue
		}
		out = append(out, accountError{
			AccountID:   nc.Account.ID,
			AccountName: nc.Account.DisplayName,
			Error:       probe.Redact(failures[i].Error()),
		})
	}
	return out
}
