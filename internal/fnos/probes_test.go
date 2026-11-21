package fnos

import (
	"context"
	"strings"
	"testing"

	"fnos-pikpak/internal/fnostest"
	"fnos-pikpak/internal/probe"
)

func TestPlatformProbeWarnsWhenForbidden(t *testing.T) {
	p := platformProbe{c: newTestClient(t, fnostest.StartForbiddenGateway(t))}

	res := p.Run(context.Background())
	if res.Status != probe.Warn {
		t.Fatalf("status = %q, want warn", res.Status)
	}
	if !strings.Contains(res.Detail, "403") {
		t.Fatalf("detail = %q, want the 403 reason", res.Detail)
	}
}

func TestPlatformProbeFailsOnOtherErrors(t *testing.T) {
	socket := fnostest.StartGateway(t, func(string, map[string]any) (int, string) { return 7, `{}` })
	p := platformProbe{c: newTestClient(t, socket)}

	res := p.Run(context.Background())
	if res.Status != probe.Fail {
		t.Fatalf("status = %q, want fail", res.Status)
	}
}