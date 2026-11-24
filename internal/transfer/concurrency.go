package transfer

import (
	"strconv"
	"strings"
)

// Concurrency settles the queue's caps from the stored "simultaneous
// downloads" setting. The per-user cap is the same number: the setting counts
// the transfers a person expects to run at once, and the global cap already
// keeps several users from oversubscribing the NAS. Anything outside 1..4
// falls back to the default.
func Concurrency(value string, fallback int) (global, perUser int) {
	n := 0
	if v, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
		n = v
	}
	if n < 1 || n > 4 {
		n = fallback
	}
	return n, n
}
