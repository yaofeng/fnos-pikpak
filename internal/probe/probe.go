package probe

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Status string

const (
	Pass Status = "pass"
	Fail Status = "fail"
	Warn Status = "warn"
	Skip Status = "skip"
)

// Result is one probe's outcome. Data carries machine-readable details for the
// M0 report; it must already be redacted.
type Result struct {
	Name   string         `json:"name"`
	Status Status         `json:"status"`
	Detail string         `json:"detail,omitempty"`
	Data   map[string]any `json:"data,omitempty"`
	RanAt  time.Time      `json:"ran_at"`
}

type Probe interface {
	Name() string
	Run(ctx context.Context) Result
}

// Registry runs probes in order. Probes may be added before serving starts.
type Registry struct {
	mu     sync.Mutex
	probes []Probe
}

func (r *Registry) Add(p Probe) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.probes = append(r.probes, p)
}

func (r *Registry) Names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.probes))
	for _, p := range r.probes {
		names = append(names, p.Name())
	}
	return names
}

// Run executes every probe, converting a panic into a failed result so one bad
// probe cannot take down the report.
func (r *Registry) Run(ctx context.Context) []Result {
	r.mu.Lock()
	probes := append([]Probe(nil), r.probes...)
	r.mu.Unlock()

	results := make([]Result, 0, len(probes))
	for _, p := range probes {
		results = append(results, runOne(ctx, p))
	}
	return results
}

func runOne(ctx context.Context, p Probe) (res Result) {
	defer func() {
		if v := recover(); v != nil {
			res = Result{
				Name:   p.Name(),
				Status: Fail,
				Detail: fmt.Sprintf("panic: %v", v),
				RanAt:  time.Now(),
			}
		}
	}()

	res = p.Run(ctx)
	if res.Name == "" {
		res.Name = p.Name()
	}
	if res.RanAt.IsZero() {
		res.RanAt = time.Now()
	}
	return res
}

type uidKey struct{}

// WithUID makes the current fnOS user available to probes.
func WithUID(ctx context.Context, uid int) context.Context {
	return context.WithValue(ctx, uidKey{}, uid)
}

func UIDFrom(ctx context.Context) (int, bool) {
	uid, ok := ctx.Value(uidKey{}).(int)
	return uid, ok
}
