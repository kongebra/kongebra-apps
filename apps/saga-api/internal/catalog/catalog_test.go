package catalog_test

import (
	"testing"

	"saga-api/internal/catalog"
)

func TestCatalogLookups(t *testing.T) {
	if _, ok := catalog.Get("qwen3.5:2b"); !ok {
		t.Fatal("local default missing")
	}
	if !catalog.IsNorwegian("deepseek-v4-flash:cloud") {
		t.Fatal("cloud must be norwegian-capable")
	}
	if catalog.IsNorwegian("qwen3.5:2b") {
		t.Fatal("qwen3.5:2b is english-only")
	}
	if catalog.IsNorwegian("nonexistent") {
		t.Fatal("unknown model must default to false")
	}
	if len(catalog.All()) < 8 {
		t.Fatalf("catalog too small: %d", len(catalog.All()))
	}
}

func TestExactlyOneDefaultPerTier(t *testing.T) {
	counts := map[string]int{}
	for _, m := range catalog.All() {
		if m.Default {
			counts[m.Tier]++
		}
	}
	if counts["local"] != 1 {
		t.Errorf("local tier defaults = %d, want 1", counts["local"])
	}
	if counts["cloud"] != 1 {
		t.Errorf("cloud tier defaults = %d, want 1", counts["cloud"])
	}
	if m, _ := catalog.Get("qwen3.5:4b"); !m.Default {
		t.Error("qwen3.5:4b should be the local default")
	}
	if m, _ := catalog.Get("deepseek-v4-flash:cloud"); !m.Default {
		t.Error("deepseek-v4-flash:cloud should be the cloud default")
	}
}
