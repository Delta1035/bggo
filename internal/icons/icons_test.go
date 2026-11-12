package icons

import "testing"

func TestLucideIconsParsed(t *testing.T) {
	icons := []struct {
		name string
		svg  interface{ Size() (float32, float32) }
	}{
		{"FolderOpen", FolderOpen},
		{"Heart", Heart},
		{"HeartFilled", HeartFilled},
		{"Code", Code},
		{"Check", Check},
		{"Download", Download},
		{"Refresh", Refresh},
		{"Clock", Clock},
		{"Trash", Trash},
		{"ExternalLink", ExternalLink},
		{"Image", Image},
		{"Globe", Globe},
		{"Eye", Eye},
		{"ChevronLeft", ChevronLeft},
		{"ChevronRight", ChevronRight},
		{"Monitor", Monitor},
		{"Sparkles", Sparkles},
	}

	for _, ic := range icons {
		if ic.svg == nil {
			t.Errorf("icon %s is nil", ic.name)
			continue
		}
		w, h := ic.svg.Size()
		if w <= 0 || h <= 0 {
			t.Errorf("icon %s has invalid size: %vx%v", ic.name, w, h)
		}
	}
}
