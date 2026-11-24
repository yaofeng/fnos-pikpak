package httpapi

import (
	"context"
	"sync"

	"fnos-pikpak/internal/pikpak"
	"fnos-pikpak/internal/store"
)

// packListing is one account's view of the places a restored copy can live:
// the drive root and the Pack From Shared folder restores land in.
type packListing struct {
	AccountID  string
	Root, Pack []pikpak.File
}

// packCopies lists every active account's root and Pack From Shared folder in
// parallel, so a copy saved by any account can be recognized later. An
// account whose session or listing fails is simply skipped — the copies that
// do exist elsewhere still count. It never fails: the caller degrades to "no
// restored copies".
func packCopies(ctx context.Context, deps Deps, uid int) []packListing {
	if deps.Store == nil || deps.Sessions == nil {
		return nil
	}
	accounts, err := deps.Store.ListAccounts(ctx, uid)
	if err != nil {
		return nil
	}
	var actives []store.PikPakAccount
	for _, a := range accounts {
		if a.Status == "active" {
			actives = append(actives, a)
		}
	}
	if len(accounts) == 0 {
		// A user bound before accounts existed owns the placeholder
		// credential and no rows; that one drive is all there is to search.
		if acct, err := deps.resolveAccount(ctx, uid, ""); err == nil {
			actives = []store.PikPakAccount{acct}
		}
	}
	listings := make([]packListing, len(actives))
	var wg sync.WaitGroup
	for i, acct := range actives {
		wg.Add(1)
		go func(i int, acct store.PikPakAccount) {
			defer wg.Done()
			client, err := deps.Sessions.ClientForAccount(ctx, uid, acct.ID)
			if err != nil {
				return
			}
			root, err := client.ListAllFiles(ctx, "")
			if err != nil {
				return
			}
			listing := packListing{AccountID: acct.ID, Root: root}
			if folder, ok := findFolder(root, packFromSharedName); ok {
				if pack, err := client.ListAllFiles(ctx, folder.ID); err == nil {
					listing.Pack = pack
				}
			}
			listings[i] = listing
		}(i, acct)
	}
	wg.Wait()
	// Drop the accounts that could not be read; bind order is kept so
	// matching is deterministic.
	out := listings[:0]
	for _, l := range listings {
		if l.Root != nil {
			out = append(out, l)
		}
	}
	return out
}

// findCopyAcross looks for a share file's existing copy across every
// account's drive. A remembered file id is matched in any account's root and
// Pack — the two places a restore can have landed. Without an id, or when it
// no longer resolves (PikPak can reissue ids), name and size take over, and
// only inside the Pack folders: a file in someone's root that merely shares
// the name is not a restored copy. Folders never match, and the size must be
// exact — unless the caller only knows the name (size 0).
func findCopyAcross(listings []packListing, rememberedID, targetName, originalName string, size int64) (pikpak.File, string, bool) {
	if rememberedID != "" {
		for _, l := range listings {
			if f, ok := findFileByID(l.Root, rememberedID); ok {
				return f, l.AccountID, true
			}
			if f, ok := findFileByID(l.Pack, rememberedID); ok {
				return f, l.AccountID, true
			}
		}
	}
	for _, l := range listings {
		for _, name := range []string{targetName, originalName} {
			if f, ok := findSameVideo(l.Pack, name, size); ok {
				return f, l.AccountID, true
			}
		}
	}
	return pikpak.File{}, "", false
}
