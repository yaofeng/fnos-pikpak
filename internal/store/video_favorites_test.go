package store

import (
	"context"
	"errors"
	"testing"
)

func videoFav(shareID, fileID, name string, size int64) VideoFavorite {
	return VideoFavorite{
		ShareID: shareID, FileID: fileID, Name: name, Size: size,
		Owner: "alice", ShareTitle: "资料",
	}
}

func TestVideoFavoritesSealThePassCode(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	fav := videoFav("VOabc123", "sf2", "a.mp4", 9)
	fav.PassCode = "7f3a"
	got, err := s.AddVideoFavorite(ctx, 1000, fav)
	if err != nil {
		t.Fatalf("AddVideoFavorite: %v", err)
	}
	if got.ID == 0 || got.Name != "a.mp4" || got.Owner != "alice" {
		t.Fatalf("favorite = %+v", got)
	}

	list, err := s.ListVideoFavorites(ctx, 1000)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %+v err=%v", list, err)
	}
	if list[0].PassCode != "" {
		t.Fatal("the list leaked the pass code")
	}

	one, err := s.GetVideoFavorite(ctx, 1000, got.ID)
	if err != nil || one.PassCode != "7f3a" {
		t.Fatalf("get = %+v err=%v", one, err)
	}
}

func TestVideoFavoritesUpsertKeepsTheOldCode(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	fav := videoFav("VOabc123", "sf2", "a.mp4", 9)
	fav.PassCode = "7f3a"
	first, err := s.AddVideoFavorite(ctx, 1000, fav)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	again, err := s.AddVideoFavorite(ctx, 1000, videoFav("VOabc123", "sf2", "a.mp4", 9))
	if err != nil {
		t.Fatalf("add again: %v", err)
	}
	if again.ID != first.ID {
		t.Fatalf("ids %d then %d", first.ID, again.ID)
	}
	one, err := s.GetVideoFavorite(ctx, 1000, first.ID)
	if err != nil || one.PassCode != "7f3a" {
		t.Fatalf("the code was erased: %+v err=%v", one, err)
	}
}

func TestVideoFavoritesArePerUser(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	got, err := s.AddVideoFavorite(ctx, 1000, videoFav("VOabc123", "sf2", "a.mp4", 9))
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := s.GetVideoFavorite(ctx, 2000, got.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user's get: %v", err)
	}
	list, err := s.ListVideoFavorites(ctx, 2000)
	if err != nil || len(list) != 0 {
		t.Fatalf("other user's list = %+v err=%v", list, err)
	}
	if err := s.DeleteVideoFavorite(ctx, 2000, got.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user's delete: %v", err)
	}
}

func TestVideoFavoriteRenameIsDisplayOnly(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	got, err := s.AddVideoFavorite(ctx, 1000, videoFav("VOabc123", "sf2", "a.mp4", 9))
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	renamed, err := s.RenameVideoFavorite(ctx, 1000, got.ID, "我的片单")
	if err != nil || renamed.DisplayName != "我的片单" || renamed.Name != "a.mp4" {
		t.Fatalf("rename = %+v err=%v", renamed, err)
	}
	if _, err := s.RenameVideoFavorite(ctx, 2000, got.ID, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user's rename: %v", err)
	}
	cleared, err := s.RenameVideoFavorite(ctx, 1000, got.ID, "")
	if err != nil || cleared.DisplayName != "" {
		t.Fatalf("clear = %+v err=%v", cleared, err)
	}
}
