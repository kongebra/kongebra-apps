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
	if len(catalog.All()) < 10 {
		t.Fatalf("catalog too small: %d", len(catalog.All()))
	}
}
