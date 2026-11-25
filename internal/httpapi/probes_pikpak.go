package httpapi

import (
	"context"
	"errors"
	"fmt"
	"time"

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
	reg.Add(&pikPakProbe{name: "pikpak.pages", sessions: sessions, run: probePages})
	reg.Add(&pikPakProbe{name: "pikpak.burst", sessions: sessions, run: probeBurst})
	reg.Add(&pikPakProbe{name: "pikpak.offline", sessions: sessions, run: probeOffline})
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
	client, err := p.sessions.ClientForAccount(ctx, uid, "")
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
		return probe.Result{
			Status: probe.Warn,
			Detail: fmt.Sprintf("%d 种排序写法都被拒绝，排序只能在前端已加载页内完成", len(results)),
			Data:   data,
		}
	}
	return probe.Result{
		Status: probe.Pass,
		Detail: fmt.Sprintf("%d/%d 种排序写法被接受", working, len(results)),
		Data:   data,
	}
}

func probeSearch(ctx context.Context, c *pikpak.Client) probe.Result {
	results := c.SearchCandidates(ctx, pikpak.SearchProbeQuery)
	data := map[string]any{"query": pikpak.SearchProbeQuery, "results": results}
	for _, r := range results {
		if r.Matched {
			return probe.Result{
				Status: probe.Pass,
				Detail: fmt.Sprintf("可用搜索端点：%s", r.Path),
				Data:   data,
			}
		}
	}
	return probe.Result{
		Status: probe.Warn,
		Detail: "没有候选真正过滤结果（q 被忽略）；搜索降级为目录内过滤",
		Data:   data,
	}
}

func probePages(ctx context.Context, c *pikpak.Client) probe.Result {
	walk := c.WalkPages(ctx, 2, 5)
	data := map[string]any{"walk": walk}
	switch walk.Stopped {
	case "error":
		return probe.Result{Status: probe.Fail, Detail: "分页请求失败：" + walk.Err, Data: data}
	case "repeated_token":
		return probe.Result{
			Status: probe.Fail,
			Detail: fmt.Sprintf("next_page_token 循环（%d 页后重复），按 token 翻页会死循环", walk.Pages),
			Data:   data,
		}
	case "max_pages":
		return probe.Result{
			Status: probe.Warn,
			Detail: fmt.Sprintf("%d 页后仍有下一页，未确认分页能正常终止", walk.Pages),
			Data:   data,
		}
	}
	if walk.DuplicateIDs > 0 {
		return probe.Result{
			Status: probe.Warn,
			Detail: fmt.Sprintf("%d 页 %d 项，存在 %d 个重复条目", walk.Pages, walk.Items, walk.DuplicateIDs),
			Data:   data,
		}
	}
	return probe.Result{
		Status: probe.Pass,
		Detail: fmt.Sprintf("%d 页 %d 项，token 正常终止", walk.Pages, walk.Items),
		Data:   data,
	}
}

func probeBurst(ctx context.Context, c *pikpak.Client) probe.Result {
	const requests = 12
	res := c.Burst(ctx, requests)
	data := map[string]any{"burst": res}
	if res.Failed > 0 {
		detail := fmt.Sprintf("%d 个并发请求中 %d 个失败", res.Requests, res.Failed)
		if res.Err != "" {
			detail += "：" + res.Err
		}
		return probe.Result{Status: probe.Warn, Detail: detail, Data: data}
	}
	return probe.Result{
		Status: probe.Pass,
		Detail: fmt.Sprintf("%d 个并发请求全部成功（%d ms）", res.Requests, res.ElapsedMS),
		Data:   data,
	}
}

// offlineCandidates are small, direct downloads. The device rejected
// https://example.com/index.html with task_support_save_snapshot, so the probe
// keeps it as evidence and falls back to a real binary and a raw file.
var offlineCandidates = []string{
	"https://example.com/index.html",
	"https://speed.hetzner.de/1MB.bin",
	"https://raw.githubusercontent.com/Quan666/PikPakAPI/main/README.md",
}

// offlineAttempt records what one candidate URL did.
type offlineAttempt struct {
	URL     string `json:"url"`
	Outcome string `json:"outcome"` // rejected | complete | error | timeout | no_task_id
	Phase   string `json:"phase,omitempty"`
	Detail  string `json:"detail,omitempty"`
	Waited  int64  `json:"waited_ms"`
	Cleaned bool   `json:"cleaned,omitempty"`
}

func probeOffline(ctx context.Context, c *pikpak.Client) probe.Result {
	attempts := make([]offlineAttempt, 0, len(offlineCandidates))

	for _, link := range offlineCandidates {
		attempt := offlineAttempt{URL: link}
		task, err := c.CreateOffline(ctx, link, "")
		if err != nil {
			attempt.Outcome = "rejected"
			attempt.Detail = probe.Redact(err.Error())
			attempts = append(attempts, attempt)
			continue
		}
		if task.ID == "" {
			attempt.Outcome = "no_task_id"
			attempts = append(attempts, attempt)
			continue
		}

		start := time.Now()
		final, waitErr := c.WaitTask(ctx, task.ID, 60*time.Second, time.Second)
		attempt.Waited = time.Since(start).Milliseconds()
		attempt.Phase = final.Phase
		cleanupErr := c.DeleteTask(ctx, task.ID, true)
		attempt.Cleaned = cleanupErr == nil

		switch {
		case waitErr == pikpak.ErrTaskTimeout:
			attempt.Outcome = "timeout"
		case waitErr != nil:
			attempt.Outcome = "error"
			attempt.Detail = probe.Redact(waitErr.Error())
		case final.Phase == "PHASE_TYPE_ERROR":
			attempt.Outcome = "error"
			attempt.Detail = final.Message
		default:
			attempt.Outcome = "complete"
		}
		attempts = append(attempts, attempt)

		if attempt.Outcome == "complete" {
			detail := fmt.Sprintf("离线任务完成（%s），等待 %d ms", link, attempt.Waited)
			if !attempt.Cleaned {
				detail += "；清理失败，请在任务列表删除本次探针任务"
			}
			return probe.Result{Status: probe.Pass, Detail: detail, Data: map[string]any{"results": attempts}}
		}
		if attempt.Outcome == "timeout" {
			// A created task is evidence enough; another URL would only add
			// more waiting.
			break
		}
	}

	data := map[string]any{"results": attempts}

	// Nothing completed: report why, most useful candidate first.
	for _, a := range attempts {
		if a.Outcome == "timeout" {
			return probe.Result{
				Status: probe.Warn,
				Detail: "离线任务已创建但 60 秒内未完成：" + a.URL,
				Data:   data,
			}
		}
	}
	last := attempts[len(attempts)-1]
	return probe.Result{
		Status: probe.Fail,
		Detail: fmt.Sprintf("%d 个候选 URL 都没能创建完成的离线任务，最后一个：%s", len(attempts), last.Detail),
		Data:   data,
	}
}
