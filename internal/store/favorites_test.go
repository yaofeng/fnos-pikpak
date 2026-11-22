package store

import (
	"context"
	"errors"
	"testing"
)

func TestShareFavoritesRoundTrip(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	fav, err := s.AddShareFavorite(ctx, 1000, "VOabc123", "大模型资料", "7f3a")
	if err != nil {
		t.Fatalf("AddShareFavorite: %v", err)
	}
	if fav.ID == 0 || !fav.Protected || fav.Title != "大模型资料" {
		t.Fatalf("favorite = %+v", fav)
	}

	list, err := s.ListShareFavorites(ctx, 1000)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %+v err=%v", list, err)
	}
	if list[0].ShareID != "VOabc123" || !list[0].Protected {
		t.Fatalf("list[0] = %+v", list[0])
	}
	if list[0].PassCode != "" {
		t.Fatal("the list leaked the pass code")
	}

	got, err := s.GetShareFavorite(ctx, 1000, fav.ID)
	if err != nil {
		t.Fatalf("GetShareFavorite: %v", err)
	}
	if got.PassCode != "7f3a" {
		t.Fatalf("pass code = %q", got.PassCode)
	}
}

func TestShareFavoritesUpsertKeepsTheStoredPassCode(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	first, err := s.AddShareFavorite(ctx, 1000, "VOabc123", "旧标题", "7f3a")
	if err != nil {
		t.Fatalf("first add: %v", err)
	}
	// Saving the same share again without a code (it was already unlocked)
	// must not wipe the code the favourite needs to reopen.
	again, err := s.AddShareFavorite(ctx, 1000, "VOabc123", "新标题", "")
	if err != nil {
		t.Fatalf("second add: %v", err)
	}
	if again.ID != first.ID || again.Title != "新标题" {
		t.Fatalf("again = %+v first = %+v", again, first)
	}
	got, _ := s.GetShareFavorite(ctx, 1000, first.ID)
	if got.PassCode != "7f3a" {
		t.Fatalf("pass code = %q", got.PassCode)
	}
	list, _ := s.ListShareFavorites(ctx, 1000)
	if len(list) != 1 {
		t.Fatalf("list = %+v", list)
	}
}

func TestShareFavoritesArePerUser(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	fav, err := s.AddShareFavorite(ctx, 1000, "VOabc123", "资料", "")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := s.GetShareFavorite(ctx, 1001, fav.ID); err == nil {
		t.Fatal("another user read the favourite")
	}
	if err := s.DeleteShareFavorite(ctx, 1001, fav.ID); err == nil {
		t.Fatal("another user deleted the favourite")
	}
	if err := s.DeleteShareFavorite(ctx, 1000, fav.ID); err != nil {
		t.Fatalf("owner delete: %v", err)
	}
	list, _ := s.ListShareFavorites(ctx, 1000)
	if len(list) != 0 {
		t.Fatalf("list = %+v", list)
	}
}

func TestShareFavoriteRenameIsDisplayOnly(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	fav, err := s.AddShareFavorite(ctx, 1000, "VOabc123", "大模型资料", "7f3a")
	if err != nil {
		t.Fatalf("AddShareFavorite: %v", err)
	}
	renamed, err := s.RenameShareFavorite(ctx, 1000, fav.ID, "网课")
	if err != nil {
		t.Fatalf("RenameShareFavorite: %v", err)
	}
	if renamed.DisplayTitle != "网课" || renamed.Title != "大模型资料" {
		t.Fatalf("renamed = %+v", renamed)
	}
	if _, err := s.RenameShareFavorite(ctx, 2000, fav.ID, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user's rename: %v", err)
	}
	cleared, err := s.RenameShareFavorite(ctx, 1000, fav.ID, "")
	if err != nil || cleared.DisplayTitle != "" {
		t.Fatalf("clear = %+v err=%v", cleared, err)
	}
}
