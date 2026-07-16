// Package catalog is the single source of truth for the model list: tiers,
// Norwegian capability (drives conditional translate), the UI meter ratings,
// and per-model price. The API serves it (GET /api/models) so the web app never
// keeps its own copy. ponytail: a static slice, not a DB table; make it a
// table only if models need editing without a redeploy.
package catalog

type Model struct {
	ID              string  `json:"id"`
	Label           string  `json:"label"`
	Tier            string  `json:"tier"` // "local" | "cloud"
	Norwegian       bool    `json:"norwegian"`
	Speed           int     `json:"speed"`     // 1-4, short-input benchmark
	Precision       int     `json:"precision"` // 1-4, short-input benchmark
	PriceInPerMtok  float64 `json:"priceInPerMtok"`
	PriceOutPerMtok float64 `json:"priceOutPerMtok"`
	Note            string  `json:"note"`
	Default         bool    `json:"default"` // the recommended pick for its tier
}

var models = []Model{
	{"deepseek-v4-flash:cloud", "DeepSeek V4 Flash", "cloud", true, 4, 4, 0, 0, "Turbo default. Best translator, large context.", true},
	{"gemini-3-flash-preview:cloud", "Gemini 3 Flash (preview)", "cloud", true, 4, 4, 0, 0, "Richest structure.", false},
	{"kimi-k2.6:cloud", "Kimi K2.6", "cloud", true, 4, 4, 0, 0, "Flawless Norwegian.", false},
	{"kimi-k2.7-code:cloud", "Kimi K2.7 Code", "cloud", true, 4, 4, 0, 0, "Flawless Norwegian.", false},
	{"glm-5.2:cloud", "GLM 5.2", "cloud", true, 3, 4, 0, 0, "Newest, clean.", false},
	{"minimax-m3:cloud", "MiniMax M3", "cloud", true, 3, 4, 0, 0, "Rich structure.", false},
	{"qwen3.5:4b", "Qwen3.5 4B", "local", false, 2, 3, 0, 0, "Local default. Best local quality (kept warm).", true},
	{"qwen3.5:2b", "Qwen3.5 2B", "local", false, 3, 2, 0, 0, "Faster local (~33% quicker), still solid English.", false},
}

// ponytail: cloud prices left 0 until Ollama Cloud publishes per-model rates;
// fill priceIn/priceOutPerMtok here and cost_usd populates automatically.

func All() []Model { return append([]Model(nil), models...) }

func Get(id string) (Model, bool) {
	for _, m := range models {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}

func IsNorwegian(id string) bool {
	m, ok := Get(id)
	return ok && m.Norwegian
}
