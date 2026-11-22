package pikpak

import (
	"context"
	"errors"
	"strings"
)

// ErrEmptySearch means the caller searched for nothing.
var ErrEmptySearch = errors.New("pikpak: empty search query")

// PathSegment names one folder on the way to a hit.
type PathSegment struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SearchHit is one matching entry and the folders it lives in.
type SearchHit struct {
	File File          `json:"file"`
	Path []PathSegment `json:"path"`
}

// SearchLimits caps a drive-wide walk: PikPak has no search endpoint (the
// list filters reject a name match), so a search reads every folder.
type SearchLimits struct {
	MaxFolders int
	MaxHits    int
}

type searchNode struct {
	id   string
	path []PathSegment
}

// SearchFiles walks the whole drive breadth-first and returns the entries
// whose name contains query, case-insensitively. scanned counts every entry
// it read; truncated reports that a limit or the context cut the walk short.
func (c *Client) SearchFiles(ctx context.Context, query string, limits SearchLimits) (hits []SearchHit, scanned int, truncated bool, err error) {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return nil, 0, false, ErrEmptySearch
	}
	if limits.MaxFolders <= 0 {
		limits.MaxFolders = 2000
	}
	if limits.MaxHits <= 0 {
		limits.MaxHits = 200
	}

	queue := []searchNode{{id: ""}}
	folders := 0
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return hits, scanned, true, err
		}
		if len(hits) >= limits.MaxHits {
			return hits, scanned, true, nil
		}
		folders++
		if folders > limits.MaxFolders {
			return hits, scanned, true, nil
		}

		node := queue[0]
		queue = queue[1:]
		token := ""
		for {
			page, err := c.ListFiles(ctx, node.id, ListOptions{Limit: 100, PageToken: token})
			if err != nil {
				return hits, scanned, truncated, err
			}
			for _, f := range page.Items {
				scanned++
				if strings.Contains(strings.ToLower(f.Name), needle) {
					if len(hits) >= limits.MaxHits {
						return hits, scanned, true, nil
					}
					hits = append(hits, SearchHit{File: f, Path: node.path})
				}
				if f.Kind == "drive#folder" {
					path := make([]PathSegment, len(node.path), len(node.path)+1)
					copy(path, node.path)
					path = append(path, PathSegment{ID: f.ID, Name: f.Name})
					queue = append(queue, searchNode{id: f.ID, path: path})
				}
			}
			token = page.NextPageToken
			if token == "" {
				break
			}
		}
	}
	return hits, scanned, false, nil
}
