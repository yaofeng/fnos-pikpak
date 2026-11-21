package probe

// NewRegistry returns an empty registry. Later milestones register concrete
// probes (fnOS gateway, file access, PikPak, upload) into it at startup.
func NewRegistry() *Registry {
	return &Registry{}
}
