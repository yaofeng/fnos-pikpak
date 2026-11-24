package transfer

import "testing"

func TestConcurrencyUsesTheSettingForBothCaps(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
	}{
		{"4", 4},
		{"1", 1},
		{"", 2},
		{"0", 2},
		{"9", 2},
		{"x", 2},
	} {
		global, perUser := Concurrency(tc.in, 2)
		if global != tc.want || perUser != tc.want {
			t.Fatalf("Concurrency(%q) = %d,%d, want %d,%d", tc.in, global, perUser, tc.want, tc.want)
		}
	}
}
