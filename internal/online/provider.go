package online

import (
	"context"
	"sync"
)

// Provider 在线壁纸源接口
type Provider interface {
	Name() string
	Categories(ctx context.Context) ([]Category, error)
	Fetch(ctx context.Context, categoryID string, page, pageSize int) ([]WallpaperItem, error)
}

// WallpaperPage carries optional pagination metadata from a source.
type WallpaperPage struct {
	Items []WallpaperItem
	More  bool
	Total int
}

// PageProvider is implemented by sources that can provide accurate
// pagination metadata in addition to the legacy item-only interface.
type PageProvider interface {
	FetchPage(ctx context.Context, categoryID string, page, pageSize int) (WallpaperPage, error)
}

// SourceRegistry stores wallpaper providers by their stable source ID.
// Providers remain deliberately small so the current Go sources and future
// sandboxed plugins can use the same routing path.
type SourceRegistry struct {
	mu      sync.RWMutex
	sources []Provider
}

// NewSourceRegistry creates an empty source registry.
func NewSourceRegistry() *SourceRegistry {
	return &SourceRegistry{}
}

// Register adds a provider unless another provider already owns its ID.
func (r *SourceRegistry) Register(provider Provider) error {
	if provider == nil {
		return ErrInvalidProvider
	}
	name := provider.Name()
	if name == "" {
		return ErrInvalidProvider
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.sources {
		if existing.Name() == name {
			return ErrDuplicateProvider
		}
	}
	r.sources = append(r.sources, provider)
	return nil
}

// Replace swaps the provider with the same stable ID, preserving its
// registration position. It is used when an embedded plugin supersedes its
// compatibility Go implementation during application startup.
func (r *SourceRegistry) Replace(provider Provider) error {
	if provider == nil || provider.Name() == "" {
		return ErrInvalidProvider
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, existing := range r.sources {
		if existing.Name() == provider.Name() {
			r.sources[i] = provider
			return nil
		}
	}
	r.sources = append(r.sources, provider)
	return nil
}

// Sources returns providers in registration order.
func (r *SourceRegistry) Sources() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	providers := make([]Provider, len(r.sources))
	copy(providers, r.sources)
	return providers
}

// Get finds a provider by its stable source ID.
func (r *SourceRegistry) Get(name string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, provider := range r.sources {
		if provider.Name() == name {
			return provider, true
		}
	}
	return nil, false
}

// Remove unregisters a source by stable ID and reports whether it existed.
func (r *SourceRegistry) Remove(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, provider := range r.sources {
		if provider.Name() == name {
			r.sources = append(r.sources[:i], r.sources[i+1:]...)
			return true
		}
	}
	return false
}
