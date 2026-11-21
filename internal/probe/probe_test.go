package probe

import (
	"context"
	"strings"
	"testing"
)

type probeFunc struct {
	name string
	run  func() Result
}

func (p probeFunc) Name() string            { return p.name }
func (p probeFunc) Run(context.Context) Result { return p.run() }

func TestRedact(t *testing.T) {
	in := `GET https://dl-a10b.mypikpak.com/file?expire=1&sign=x&userid=2 Authorization: Bearer abc.def`
	out := Redact(in)
	for _, bad := range []string{"sign=x", "userid=2", "abc.def"} {
		if strings.Contains(out, bad) {
			t.Fatalf("leaked %q in %q", bad, out)
		}
	}
	if !strings.Contains(out, "dl-a10b.mypikpak.com/file") {
		t.Fatalf("lost host: %q", out)
	}
}

func TestRedactKeyValueForms(t *testing.T) {
	cases := []string{
		`{"access_token":"aa.bb.cc"}`,
		`{"refresh_token": "rr.ss.tt"}`,
		`device_code: dd-ee-ff`,
		`pass_code_token=ppqq`,
	}
	for _, in := range cases {
		out := Redact(in)
		for _, leaked := range []string{"aa.bb.cc", "rr.ss.tt", "dd-ee-ff", "ppqq"} {
			if strings.Contains(in, leaked) && strings.Contains(out, leaked) {
				t.Fatalf("leaked %q in %q", leaked, out)
			}
		}
	}
}

func TestRegistryRecoversPanic(t *testing.T) {
	r := &Registry{}
	r.Add(probeFunc{name: "boom", run: func() Result { panic("kaboom") }})

	results := r.Run(context.Background())
	if len(results) != 1 {
		t.Fatalf("results = %d", len(results))
	}
	if results[0].Status != Fail {
		t.Fatalf("status = %q, want fail", results[0].Status)
	}
	if !strings.Contains(results[0].Detail, "kaboom") {
		t.Fatalf("detail = %q", results[0].Detail)
	}
}
