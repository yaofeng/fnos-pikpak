package fnos

import (
	"context"

	"fnos-pikpak/internal/probe"
)

// RegisterProbes adds the fnOS integration probes. They skip when the Open API
// client is missing or the request has no user identity.
func RegisterProbes(reg *probe.Registry, c *Client) {
	reg.Add(platformProbe{c: c})
	reg.Add(foldersProbe{c: c})
}

type platformProbe struct{ c *Client }

func (p platformProbe) Name() string { return "fnos.platform" }

func (p platformProbe) Run(ctx context.Context) probe.Result {
	if p.c == nil {
		return probe.Result{Name: p.Name(), Status: probe.Skip, Detail: "Open API 客户端未配置"}
	}
	cfg, err := p.c.GetPlatformConfig(ctx)
	if err != nil {
		return probe.Result{Name: p.Name(), Status: probe.Fail, Detail: err.Error()}
	}
	return probe.Result{
		Name:   p.Name(),
		Status: probe.Pass,
		Data:   map[string]any{"language": cfg.Language, "version": cfg.Version},
	}
}

type foldersProbe struct{ c *Client }

func (p foldersProbe) Name() string { return "fnos.folders" }

func (p foldersProbe) Run(ctx context.Context) probe.Result {
	if p.c == nil {
		return probe.Result{Name: p.Name(), Status: probe.Skip, Detail: "Open API 客户端未配置"}
	}
	uid, ok := probe.UIDFrom(ctx)
	if !ok {
		return probe.Result{Name: p.Name(), Status: probe.Skip, Detail: "缺少用户身份"}
	}

	paths, err := p.c.GetUserAccessibleFolders(ctx, uid)
	if err != nil {
		return probe.Result{Name: p.Name(), Status: probe.Fail, Detail: err.Error()}
	}

	data := map[string]any{"count": len(paths), "paths": paths}
	if len(paths) > 0 {
		if semantic, err := p.c.ConvertPath(ctx, paths, "zh-CN"); err == nil {
			data["semantic"] = semantic
		}
	}
	return probe.Result{Name: p.Name(), Status: probe.Pass, Data: data}
}
