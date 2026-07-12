package llm_test

import (
	"strings"
	"testing"

	"saga-api/internal/catalog"
)

// TestCatalogTierMatchesRoutingSuffix fences the coupling between two
// independent pieces of logic that must never disagree: catalog.Model.Tier
// (which the API uses to pick an enqueue capacity pool) and the Router's
// isCloudModel suffix check (which picks the actual backend at dispatch
// time). Nothing in the type system ties them together, so a catalog edit
// that adds a "-cloud"/":cloud" id without tier: "cloud" (or vice versa)
// would silently enqueue a job into the wrong capacity pool while the Router
// still dispatches it correctly (or vice versa) - this test catches that at
// build time instead of in production.
//
// The suffix check is inlined here (mirroring internal/llm/router.go's
// unexported isCloudModel) rather than imported, since it is unexported and
// this test's job is to hold the catalog to the routing CONTRACT, not to
// exercise the Router's implementation.
func TestCatalogTierMatchesRoutingSuffix(t *testing.T) {
	for _, m := range catalog.All() {
		routesToCloud := strings.HasSuffix(m.ID, ":cloud") || strings.HasSuffix(m.ID, "-cloud")
		isCloudTier := m.Tier == "cloud"
		if routesToCloud != isCloudTier {
			t.Errorf("model %q: tier=%q (isCloudTier=%v) but routesToCloud=%v - tier and routing suffix disagree",
				m.ID, m.Tier, isCloudTier, routesToCloud)
		}
	}
}
