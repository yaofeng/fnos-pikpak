package fnos

import (
	"context"
	"strings"
	"testing"

	"fnos-pikpak/internal/fnostest"
	"fnos-pikpak/internal/probe"
)

func TestProbesSkipWithoutAnFnosClient(t *testing.T) {
	// Local development has no fnOS gateway socket; the probes must say so
	// instead of reporting a connection error.
	reg := &probe.Registry{}
	RegisterProbes(reg, nil)

	results := reg.Run(context.Background())
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	for _, r := range results {
		if r.Status != probe.Skip {
			t.Fatalf("%s status = %s (%s), want skip", r.Name, r.Status, r.Detail)
		}
	}
}

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