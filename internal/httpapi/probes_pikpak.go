package httpapi

import (
	"context"
	"errors"
	"fmt"

	"fnos-pikpak/internal/pikpak"
	"fnos-pikpak/internal/probe"
)

// RegisterPikPakProbes attaches the M0 upstream probes. They live in this
// package (not probe/builtin.go) because they need the per-user SessionStore,
// and keeping probe free of pikpak avoids an import cycle later.
func RegisterPikPakProbes(reg *probe.Registry, sessions *pikpak.SessionStore) {
	if reg == nil || sessions == nil {
		return
	}
	reg.Add(&pikPakProbe{name: "pikpak.list", sessions: sessions, run: probeList})
	reg.Add(&pikPakProbe{name: "pikpak.quota", sessions: sessions, run: probeQuota})
	reg.Add(&pikPakProbe{name: "pikpak.vip", sessions: sessions, run: probeVIP})
	reg.Add(&pikPakProbe{name: "pikpak.order", sessions: sessions, run: probeOrder})
	reg.Add(&pikPakProbe{name: "pikpak.search", sessions: sessions, run: probeSearch})
}

type pikPakProbe struct {
	name     string
	sessions *pikpak.SessionStore
	run      func(context.Context, *pikpak.Client) probe.Result
}

func (p *pikPakProbe) Name() string { return p.name }

func (p *pikPakProbe) Run(ctx context.Context) probe.Result {
	uid, ok := probe.UIDFrom(ctx)
	if !ok {
		return probe.Result{Name: p.name, Status: probe.Skip, Detail: "没有飞牛用户上下文"}
	}
	client, err := p.sessions.ClientFor(ctx, uid)
	if errors.Is(err, pikpak.ErrNotBound) {
		return probe.Result{Name: p.name, Status: probe.Skip, Detail: "未绑定 PikPak 账号，请先在上方绑定"}
	}
	if err != nil {
		return probe.Result{Name: p.name, Status: probe.Fail, Detail: probe.Redact(err.Error())}
	}
	res := p.run(ctx, client)
	res.Name = p.name
	return res
}

func probeList(ctx context.Context, c *pikpak.Client) probe.Result {
	page, err := c.ListFiles(ctx, "", pikpak.ListOptions{Limit: 10})
	if err != nil {
		return probe.Result{Status: probe.Fail, Detail: "列目录失败：" + probe.Redact(err.Error())}
	}
	items := make([]map[string]any, 0, len(page.Items))
	for i, f := range page.Items {
		if i >= 10 {
			break
		}
		items = append(items, map[string]any{
			"name":  f.Name,
			"kind":  f.Kind,
			"size":  f.Size,
			"phase": f.Phase,
		})
	}
	return probe.Result{
		Status: probe.Pass,
		Detail: fmt.Sprintf("根目录返回 %d 项", len(page.Items)),
		Data: map[string]any{
			"items":           items,
			"next_page_token": page.NextPageToken != "",
		},
	}
}

func probeQuota(ctx context.Context, c *pikpak.Client) probe.Result {
	about, err := c.About(ctx)
	if err != nil {
		return probe.Result{Status: probe.Fail, Detail: "查询配额失败：" + probe.Redact(err.Error())}
	}
	data := map[string]any{
		"quota_limit": about.QuotaLimit,
		"quota_usage": about.QuotaUsage,
		"expires_at":  about.ExpiresAt,
	}
	if about.QuotaLimit <= 0 {
		return probe.Result{Status: probe.Warn, Detail: "配额字段为空或为 0", Data: data}
	}
	return probe.Result{Status: probe.Pass, Detail: fmt.Sprintf("配额 %d，已用 %d", about.QuotaLimit, about.QuotaUsage), Data: data}
}

func probeVIP(ctx context.Context, c *pikpak.Client) probe.Result {
	vip, err := c.VIP(ctx)
	if err != nil {
		return probe.Result{Status: probe.Fail, Detail: "查询会员失败：" + probe.Redact(err.Error())}
	}
	data := map[string]any{"type": vip.Type, "expire": vip.Expire}
	if vip.Type == "" {
		return probe.Result{Status: probe.Warn, Detail: "会员字段为空", Data: data}
	}
	return probe.Result{Status: probe.Pass, Detail: "会员类型：" + vip.Type, Data: data}
}

func probeOrder(ctx context.Context, c *pikpak.Client) probe.Result {
	results := c.OrderCandidates(ctx)
	working := 0
	for _, r := range results {
		if r.Status == 200 && r.Err == "" {
			working++
		}
	}
	data := map[string]any{"results": results}
	if working == 0 {
		return probe.Result{Status: probe.Warn, Detail: "4 种排序参数均被拒绝，排序只能在前端已加载页内完成", Data: data}
	}
	return probe.Result{Status: probe.Pass, Detail: fmt.Sprintf("%d/4 种排序参数被接受", working), Data: data}
}

func probeSearch(ctx context.Context, c *pikpak.Client) probe.Result {
	results := c.SearchCandidates(ctx, "test")
	data := map[string]any{"query": "test", "results": results}
	for _, r := range results {
		if r.LooksLikeFileList {
			return probe.Result{
				Status: probe.Pass,
				Detail: fmt.Sprintf("可用搜索端点：%s", r.Path),
				Data:   data,
			}
		}
	}
	return probe.Result{Status: probe.Warn, Detail: "4 个候选搜索端点都不可用（搜索降级为目录内过滤）", Data: data}
}
