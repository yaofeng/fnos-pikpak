package sharelink

import "testing"

func TestHostAcceptsShareDomainsAndSubdomains(t *testing.T) {
	cases := map[string]bool{
		"mypikpak.com":        true,
		"www.mypikpak.com":    true,
		"mypikpak.net":        true,
		"pikpak.me":           true,
		"mypikpak.me":         true,
		"MYPIKPAK.COM":        true,
		"mypikpak.com.evil":   false,
		"notmypikpak.com":     false,
		"google.com":          false,
		"mypikpak.com.cn.evi": false,
	}
	for host, want := range cases {
		if got := Host(host); got != want {
			t.Fatalf("Host(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestIDReadsTheShareOutOfALink(t *testing.T) {
	id := "VOzKdR-2i9kQWNDeg0O6kQGuo2"
	got, ok := ID("https://mypikpak.com/s/" + id)
	if !ok || got != id {
		t.Fatalf("ID = %q, %v", got, ok)
	}
}

func TestIDRejectsPagesThatAreNotShares(t *testing.T) {
	for _, raw := range []string{
		"https://mypikpak.com/",
		"https://mypikpak.com/help",
		"https://mypikpak.com/s/",
		"https://example.com/s/GQnXxT2o5qDNIWou9I6jMQch3",
		"not a url",
	} {
		if id, ok := ID(raw); ok {
			t.Fatalf("ID(%q) = %q, want no share", raw, id)
		}
	}
}

// A search lists whatever id the provider gave; opening it is what checks the
// shape.
func TestIDDoesNotJudgeTheIDShape(t *testing.T) {
	if id, ok := ID("https://mypikpak.com/s/VN1"); !ok || id != "VN1" {
		t.Fatalf("ID = %q, %v", id, ok)
	}
	if ValidID("VN1") {
		t.Fatal("a three-character id passed ValidID")
	}
	if !ValidID("VOzKdR-2i9kQWNDeg0O6kQGuo2") {
		t.Fatal("a real id failed ValidID")
	}
}
