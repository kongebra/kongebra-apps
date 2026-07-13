# Saga Web Phase 2a - Frontend Redesign + Model Tiers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild the saga-web UI around the model catalog (`/api/models`): a hero composer with a Lokal/Turbo tier toggle + Advanced picker (speed/precision meters), a job-card dashboard, and a drawer for job detail - no forced navigation on submit.

**Architecture:** Route restructure to a pathless layout (`_dash`) that owns the always-mounted dashboard and renders the drawer as a child outlet, so `/` and `/jobs/$id` share one mounted dashboard (deep-load renders the dashboard behind the open drawer). Model selection is driven entirely by the server catalog; the browser keeps no hardcoded model list. Pure logic (catalog helpers, selection persistence, meter segments) lives in `src/lib/*` with `node:test` tests; components are covered by `tsc --noEmit`, `vite build`, and a final Playwright E2E pass.

**Tech Stack:** TanStack Start (react-start), React 19, Tailwind v4, radix-ui v1.6.1 (Dialog for the drawer, ToggleGroup, Tooltip, Popover), lucide-react, marked + DOMPurify. No new dependencies.

## Global Constraints

- **Scope is 2a only.** Compare view (spec §4.6) and its pairwise-preference backend are Phase 2b - do NOT build them.
- **Single source of truth:** the browser never hardcodes a model list. All models come from `GET /api/models`. Deleting the `MODELS` const in `index.tsx` is mandatory.
- **No new npm dependencies.** Use radix-ui primitives already present (`Dialog`, `ToggleGroup`, `Tooltip`, `Popover`), lucide icons, and existing shadcn wrappers.
- **Reserved accents (spec §4.1):** Brand = Nordic teal `oklch(0.72 0.12 175)` light / `oklch(0.78 0.13 175)` dark. Turbo = amber `oklch(0.72 0.15 70)` light / `oklch(0.82 0.16 75)` dark, used ONLY for the Turbo state - nowhere else. Meters/body stay neutral. Status colors are `--status-*` light/dark CSS-var pairs, not raw tailwind shades.
- **Norwegian UI copy** for user-facing labels (matches existing app); code identifiers and URL paths stay English.
- **No em-dash** in any text or copy - use a plain hyphen.
- **Mark deliberate simplifications** with a `// ponytail:` comment (what + upgrade path).
- **Optimistic insert on submit is mandatory; no redirect.** POST success prepends a `queued` card, clears+refocuses the input, and reconciles on the next 5s poll.
- **Cloud availability is server-authoritative:** the Turbo segment is disabled with a tooltip when cloud is unconfigured, but a cloud model with no key already errors clearly server-side - the UI gate is convenience, not the enforcement.
- **Tests run with** `npx tsx --test <file>` (node:test); this is the only test runner in the repo. No jsdom/testing-library.
- **Path alias:** `@/` resolves to `src/` (existing tsconfig). Use it for imports as the current code does.

---

## File Structure

**Backend (one small change):**
- Modify: `apps/saga-api/internal/catalog/catalog.go` - add `Default bool` field + values
- Modify: `apps/saga-api/internal/catalog/catalog_test.go` - assert exactly one default per tier

**Frontend new files:**
- `apps/saga-web/src/lib/catalog.ts` - `Model` type, `fetchModels()`, pure helpers `byTier`, `tierDefault`
- `apps/saga-web/src/lib/catalog.test.ts`
- `apps/saga-web/src/lib/selection.ts` - localStorage tier+model persistence, `resolveSelection`
- `apps/saga-web/src/lib/selection.test.ts`
- `apps/saga-web/src/lib/meters.ts` - `meterSegments`, `meterLabel`
- `apps/saga-web/src/lib/meters.test.ts`
- `apps/saga-web/src/components/meter.tsx` - `<Meter>` bar mark
- `apps/saga-web/src/components/tier-toggle.tsx` - `<TierToggle>`
- `apps/saga-web/src/components/model-picker.tsx` - `<ModelPicker>` (Advanced popover)
- `apps/saga-web/src/components/composer.tsx` - `<Composer>` (hero form)
- `apps/saga-web/src/components/job-card.tsx` - `<JobCard>` grid card
- `apps/saga-web/src/components/ui/sheet.tsx` - shadcn Sheet (radix Dialog) wrapper
- `apps/saga-web/src/components/ui/tooltip.tsx` - shadcn Tooltip wrapper
- `apps/saga-web/src/routes/_dash.tsx` - pathless layout: Shell + Dashboard + `<Outlet/>`
- `apps/saga-web/src/routes/_dash.index.tsx` - `/` leaf (no drawer)
- `apps/saga-web/src/routes/_dash.jobs.$id.tsx` - `/jobs/$id` drawer

**Frontend modified/removed:**
- Modify: `apps/saga-web/src/styles.css` - add `--brand`, `--turbo`, `--status-*` vars + `@theme inline` entries
- Modify: `apps/saga-web/src/ui.tsx` - `StatusPill` uses `--status-*`; `Shell` keeps header
- Modify: `apps/saga-web/src/types.ts` - `Model` re-export path note (types stay)
- Delete: `apps/saga-web/src/routes/index.tsx` (replaced by `_dash.index.tsx` + `_dash.tsx`)
- Delete: `apps/saga-web/src/routes/jobs.$id.tsx` (replaced by `_dash.jobs.$id.tsx`)
- `apps/saga-web/src/routeTree.gen.ts` regenerates on `vite dev`/`build` - do not hand-edit.

---

## Task 1: Backend - `Default` flag in catalog

**Files:**
- Modify: `apps/saga-api/internal/catalog/catalog.go`
- Test: `apps/saga-api/internal/catalog/catalog_test.go`

**Interfaces:**
- Produces: `catalog.Model` gains `Default bool` with JSON tag `default`. Exactly one `Default:true` model per tier (`deepseek-v4-flash:cloud` for cloud, `qwen3.5:2b` for local). The web app reads this to mark the "Anbefalt" badge and to pick a tier default on first visit / stale-selection fallback.

- [ ] **Step 1: Write the failing test**

Add to `apps/saga-api/internal/catalog/catalog_test.go`:

```go
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
	if m, _ := catalog.Get("qwen3.5:2b"); !m.Default {
		t.Error("qwen3.5:2b should be the local default")
	}
	if m, _ := catalog.Get("deepseek-v4-flash:cloud"); !m.Default {
		t.Error("deepseek-v4-flash:cloud should be the cloud default")
	}
}
```

(If the test file has no `import "saga-api/internal/catalog"` yet, add it.)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/saga-api && go test ./internal/catalog/ -run TestExactlyOneDefaultPerTier -count=1`
Expected: FAIL (Model has no field `Default`).

- [ ] **Step 3: Add the field + values**

In `catalog.go`, add the field to the struct (after `Note`):

```go
	Note            string  `json:"note"`
	Default         bool    `json:"default"` // the recommended pick for its tier
```

Then set `Default` on the two rows. The struct literals are positional, so append `, true` / `, false` as the final field on every row. The two defaults:

```go
	{"deepseek-v4-flash:cloud", "DeepSeek V4 Flash", "cloud", true, 4, 4, 0, 0, "Turbo default. Best translator, large context.", true},
```
```go
	{"qwen3.5:2b", "Qwen3.5 2B", "local", false, 2, 3, 0, 0, "Local default. Excellent structured English.", true},
```

Every other row ends with `, false`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd apps/saga-api && go test ./internal/catalog/ -count=1 && go build ./...`
Expected: PASS + build OK.

- [ ] **Step 5: Commit**

```bash
git add apps/saga-api/internal/catalog/
git commit -m "feat(saga-api): mark recommended model per tier (catalog.Default)"
```

---

## Task 2: Frontend catalog client + helpers

**Files:**
- Create: `apps/saga-web/src/lib/catalog.ts`
- Test: `apps/saga-web/src/lib/catalog.test.ts`

**Interfaces:**
- Produces:
  - `type Model = { id: string; label: string; tier: "local" | "cloud"; norwegian: boolean; speed: number; precision: number; priceInPerMtok: number; priceOutPerMtok: number; note: string; default: boolean }`
  - `type Tier = "local" | "cloud"`
  - `async function fetchModels(): Promise<Model[]>` - browser GET `/api/models`, returns `body.models ?? []`
  - `function byTier(models: Model[], tier: Tier): Model[]`
  - `function tierDefault(models: Model[], tier: Tier): Model | undefined` - the `default:true` model for that tier, else the first model of that tier
  - `function cloudAvailable(models: Model[]): boolean` - true when at least one cloud model is present (the server only lists cloud models when the key is set - see Task 1 note below)

**Note for implementer:** `/api/models` currently returns all cloud models regardless of key state. `cloudAvailable` therefore cannot infer key state from the catalog alone. Define `cloudAvailable` to accept an explicit flag instead: `function cloudAvailable(cloudConfigured: boolean): boolean { return cloudConfigured }` is pointless - so INSTEAD expose cloud-config via a separate field. Resolve this in Step 1 by having `fetchModels` also read a top-level `cloud_enabled` boolean from the response, returning `{ models, cloudEnabled }`. See Step 3.

- [ ] **Step 1: Write the failing test**

Create `apps/saga-web/src/lib/catalog.test.ts`:

```ts
import { test } from "node:test"
import assert from "node:assert/strict"
import { byTier, tierDefault, type Model } from "./catalog"

const M = (over: Partial<Model>): Model => ({
  id: "x", label: "X", tier: "local", norwegian: false, speed: 1,
  precision: 1, priceInPerMtok: 0, priceOutPerMtok: 0, note: "", default: false,
  ...over,
})

const models: Model[] = [
  M({ id: "a", tier: "local", default: false }),
  M({ id: "b", tier: "local", default: true }),
  M({ id: "c", tier: "cloud", default: true }),
  M({ id: "d", tier: "cloud", default: false }),
]

test("byTier filters by tier", () => {
  assert.deepEqual(byTier(models, "local").map((m) => m.id), ["a", "b"])
  assert.deepEqual(byTier(models, "cloud").map((m) => m.id), ["c", "d"])
})

test("tierDefault returns the default row", () => {
  assert.equal(tierDefault(models, "local")?.id, "b")
  assert.equal(tierDefault(models, "cloud")?.id, "c")
})

test("tierDefault falls back to first of tier when none flagged", () => {
  const noneFlagged = models.map((m) => ({ ...m, default: false }))
  assert.equal(tierDefault(noneFlagged, "local")?.id, "a")
})

test("tierDefault is undefined when tier is empty", () => {
  assert.equal(tierDefault([], "local"), undefined)
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/saga-web && npx tsx --test src/lib/catalog.test.ts`
Expected: FAIL (cannot find module `./catalog`).

- [ ] **Step 3: Implement**

Create `apps/saga-web/src/lib/catalog.ts`:

```ts
export type Tier = "local" | "cloud"

export interface Model {
  id: string
  label: string
  tier: Tier
  norwegian: boolean
  speed: number
  precision: number
  priceInPerMtok: number
  priceOutPerMtok: number
  note: string
  default: boolean
}

// The server owns the model list; the browser never hardcodes it. cloudEnabled
// reflects whether OLLAMA_API_KEY is configured server-side (drives the Turbo
// gate). ponytail: the API adds `cloud_enabled` alongside `models`; until then
// it defaults true (cloud models are always listed). Wired in Task 12's API note.
export async function fetchModels(): Promise<{ models: Model[]; cloudEnabled: boolean }> {
  const res = await fetch("/api/models")
  if (!res.ok) throw new Error(`/api/models returned ${res.status}`)
  const body = (await res.json()) as { models?: Model[]; cloud_enabled?: boolean }
  return { models: body.models ?? [], cloudEnabled: body.cloud_enabled ?? true }
}

export function byTier(models: Model[], tier: Tier): Model[] {
  return models.filter((m) => m.tier === tier)
}

export function tierDefault(models: Model[], tier: Tier): Model | undefined {
  const t = byTier(models, tier)
  return t.find((m) => m.default) ?? t[0]
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd apps/saga-web && npx tsx --test src/lib/catalog.test.ts && npx tsc --noEmit`
Expected: 4 pass + typecheck clean.

- [ ] **Step 5: Commit**

```bash
git add apps/saga-web/src/lib/catalog.ts apps/saga-web/src/lib/catalog.test.ts
git commit -m "feat(saga-web): catalog client + tier helpers"
```

---

## Task 3: Selection persistence

**Files:**
- Create: `apps/saga-web/src/lib/selection.ts`
- Test: `apps/saga-web/src/lib/selection.test.ts`

**Interfaces:**
- Consumes: `Model`, `Tier`, `tierDefault` from `./catalog`
- Produces:
  - `type Selection = { tier: Tier; modelId: string }`
  - `function resolveSelection(models: Model[], stored: Partial<Selection> | null): Selection` - validates a stored selection against the live catalog. If the stored model still exists, keep it and set tier from that model. Otherwise fall back to the stored tier's default (or local default). Never returns a modelId absent from `models`.
  - `const STORAGE_KEY = "saga.selection"`
  - `function loadStored(): Partial<Selection> | null` - safe `localStorage` read (returns null on SSR/parse error)
  - `function saveSelection(sel: Selection): void` - safe `localStorage` write (no-op on SSR)

- [ ] **Step 1: Write the failing test**

Create `apps/saga-web/src/lib/selection.test.ts`:

```ts
import { test } from "node:test"
import assert from "node:assert/strict"
import { resolveSelection } from "./selection"
import type { Model } from "./catalog"

const M = (id: string, tier: "local" | "cloud", def = false): Model => ({
  id, label: id, tier, norwegian: false, speed: 1, precision: 1,
  priceInPerMtok: 0, priceOutPerMtok: 0, note: "", default: def,
})

const models: Model[] = [
  M("qwen3.5:2b", "local", true),
  M("qwen3.5:4b", "local"),
  M("deepseek-v4-flash:cloud", "cloud", true),
]

test("keeps a still-valid stored model and syncs tier", () => {
  const sel = resolveSelection(models, { tier: "local", modelId: "deepseek-v4-flash:cloud" })
  assert.equal(sel.modelId, "deepseek-v4-flash:cloud")
  assert.equal(sel.tier, "cloud") // tier follows the model, not the stale stored tier
})

test("falls back to stored tier default when model was dropped", () => {
  const sel = resolveSelection(models, { tier: "cloud", modelId: "gone:cloud" })
  assert.equal(sel.modelId, "deepseek-v4-flash:cloud")
  assert.equal(sel.tier, "cloud")
})

test("defaults to local default when nothing stored", () => {
  const sel = resolveSelection(models, null)
  assert.equal(sel.tier, "local")
  assert.equal(sel.modelId, "qwen3.5:2b")
})

test("empty catalog yields local tier with empty modelId (no crash)", () => {
  const sel = resolveSelection([], null)
  assert.equal(sel.tier, "local")
  assert.equal(sel.modelId, "")
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/saga-web && npx tsx --test src/lib/selection.test.ts`
Expected: FAIL (cannot find module `./selection`).

- [ ] **Step 3: Implement**

Create `apps/saga-web/src/lib/selection.ts`:

```ts
import { tierDefault, type Model, type Tier } from "./catalog"

export interface Selection {
  tier: Tier
  modelId: string
}

export const STORAGE_KEY = "saga.selection"

// Reconcile a stored selection against the live catalog. A stored model that
// still exists wins (and pins the tier to that model's tier); a dropped model
// falls back to the stored tier's default. Guarantees the returned modelId is
// always one the catalog currently serves (or "" for an empty catalog).
export function resolveSelection(models: Model[], stored: Partial<Selection> | null): Selection {
  const storedTier: Tier = stored?.tier === "cloud" ? "cloud" : "local"
  if (stored?.modelId) {
    const hit = models.find((m) => m.id === stored.modelId)
    if (hit) return { tier: hit.tier, modelId: hit.id }
  }
  const def = tierDefault(models, storedTier)
  return { tier: storedTier, modelId: def?.id ?? "" }
}

export function loadStored(): Partial<Selection> | null {
  if (typeof localStorage === "undefined") return null
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    return raw ? (JSON.parse(raw) as Partial<Selection>) : null
  } catch {
    return null
  }
}

export function saveSelection(sel: Selection): void {
  if (typeof localStorage === "undefined") return
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(sel))
  } catch {
    // ponytail: ignore quota/private-mode write failures; selection just
    // won't persist. No user-facing error for a non-critical preference.
  }
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd apps/saga-web && npx tsx --test src/lib/selection.test.ts && npx tsc --noEmit`
Expected: 4 pass + typecheck clean.

- [ ] **Step 5: Commit**

```bash
git add apps/saga-web/src/lib/selection.ts apps/saga-web/src/lib/selection.test.ts
git commit -m "feat(saga-web): localStorage model selection with catalog reconciliation"
```

---

## Task 4: Palette + status vars

**Files:**
- Modify: `apps/saga-web/src/styles.css`
- Modify: `apps/saga-web/src/ui.tsx` (StatusPill)

**Interfaces:**
- Produces: Tailwind color utilities `bg-brand`, `text-brand`, `ring-brand`, `bg-turbo`, `text-turbo`, and `--status-{queued,running,done,failed}` var pairs consumed as `bg-[var(--status-running)]` etc. `StatusPill` renders with these vars.

- [ ] **Step 1: Add CSS variables**

In `styles.css`, add to `:root` (light):

```css
  --brand: oklch(0.72 0.12 175);
  --turbo: oklch(0.72 0.15 70);
  --status-queued: oklch(0.556 0 0);
  --status-running: oklch(0.62 0.14 250);
  --status-done: oklch(0.6 0.13 150);
  --status-failed: oklch(0.577 0.245 27.325);
```

Add to `.dark`:

```css
  --brand: oklch(0.78 0.13 175);
  --turbo: oklch(0.82 0.16 75);
  --status-queued: oklch(0.708 0 0);
  --status-running: oklch(0.7 0.14 250);
  --status-done: oklch(0.72 0.14 150);
  --status-failed: oklch(0.704 0.191 22.216);
```

Add to `@theme inline`:

```css
  --color-brand: var(--brand);
  --color-turbo: var(--turbo);
```

- [ ] **Step 2: Refactor StatusPill to status vars**

In `ui.tsx`, replace `STATUS_STYLE` and `STATUS_LABEL`:

```tsx
const STATUS_STYLE: Record<JobStatus, string> = {
  queued: "bg-[var(--status-queued)] text-white",
  running: "bg-[var(--status-running)] text-white",
  done: "bg-[var(--status-done)] text-white",
  failed: "bg-[var(--status-failed)] text-white",
}

const STATUS_LABEL: Record<JobStatus, string> = {
  queued: "I kø",
  running: "Kjører",
  done: "Ferdig",
  failed: "Feilet",
}

export function StatusPill({ status }: { status: JobStatus }) {
  return <Badge className={cn(STATUS_STYLE[status])}>{STATUS_LABEL[status]}</Badge>
}
```

(Remove the now-unused `capitalize` class since labels are Norwegian, not the raw status word.)

- [ ] **Step 3: Verify build + typecheck**

Run: `cd apps/saga-web && npx tsc --noEmit && npx vite build`
Expected: build succeeds. (No unit test - CSS/visual, covered by E2E in Task 12.)

- [ ] **Step 4: Commit**

```bash
git add apps/saga-web/src/styles.css apps/saga-web/src/ui.tsx
git commit -m "feat(saga-web): brand/turbo/status palette vars + Norwegian status pills"
```

---

## Task 5: Meter mark (segments + component)

**Files:**
- Create: `apps/saga-web/src/lib/meters.ts`
- Test: `apps/saga-web/src/lib/meters.test.ts`
- Create: `apps/saga-web/src/components/meter.tsx`

**Interfaces:**
- Produces:
  - `function meterSegments(value: number, max?: number): boolean[]` - length `max` (default 4), `value` clamped to `[0, max]`, first `value` entries `true`.
  - `function meterLabel(kind: "speed" | "precision", value: number, max?: number): string` - e.g. `"Speed 3 of 4"`, `"Presisjon 4 of 4"`.
  - `<Meter kind="speed" | "precision" value={n} />` - renders the bar mark per spec §4.3: lucide icon prefix (`Gauge` speed, `Target` precision) + 4 bars `w-1.5 h-3.5 rounded-[2px] gap-[3px]`, filled `bg-foreground/85`, empty `bg-foreground/12`, wrapper `role="img" aria-label={meterLabel(...)}`, trailing numeric `n/4`.

- [ ] **Step 1: Write the failing test**

Create `apps/saga-web/src/lib/meters.test.ts`:

```ts
import { test } from "node:test"
import assert from "node:assert/strict"
import { meterSegments, meterLabel } from "./meters"

test("meterSegments fills first N of 4", () => {
  assert.deepEqual(meterSegments(3), [true, true, true, false])
  assert.deepEqual(meterSegments(0), [false, false, false, false])
  assert.deepEqual(meterSegments(4), [true, true, true, true])
})

test("meterSegments clamps out-of-range", () => {
  assert.deepEqual(meterSegments(7), [true, true, true, true])
  assert.deepEqual(meterSegments(-2), [false, false, false, false])
})

test("meterLabel formats kind + value", () => {
  assert.equal(meterLabel("speed", 3), "Speed 3 of 4")
  assert.equal(meterLabel("precision", 4), "Presisjon 4 of 4")
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/saga-web && npx tsx --test src/lib/meters.test.ts`
Expected: FAIL (cannot find module `./meters`).

- [ ] **Step 3: Implement the logic**

Create `apps/saga-web/src/lib/meters.ts`:

```ts
export function meterSegments(value: number, max = 4): boolean[] {
  const n = Math.max(0, Math.min(max, Math.round(value)))
  return Array.from({ length: max }, (_, i) => i < n)
}

export function meterLabel(kind: "speed" | "precision", value: number, max = 4): string {
  const label = kind === "speed" ? "Speed" : "Presisjon"
  return `${label} ${Math.max(0, Math.min(max, Math.round(value)))} of ${max}`
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd apps/saga-web && npx tsx --test src/lib/meters.test.ts`
Expected: 3 pass.

- [ ] **Step 5: Implement the component**

Create `apps/saga-web/src/components/meter.tsx`:

```tsx
import { Gauge, Target } from "lucide-react"
import { meterSegments, meterLabel } from "@/lib/meters"
import { cn } from "@/lib/utils"

export function Meter({ kind, value }: { kind: "speed" | "precision"; value: number }) {
  const Icon = kind === "speed" ? Gauge : Target
  const segs = meterSegments(value)
  return (
    <span className="inline-flex items-center gap-1.5 tabular-nums text-muted-foreground">
      <Icon className="size-3" aria-hidden />
      <span role="img" aria-label={meterLabel(kind, value)} className="flex items-center gap-[3px]">
        {segs.map((on, i) => (
          <span
            key={i}
            className={cn("h-3.5 w-1.5 rounded-[2px]", on ? "bg-foreground/85" : "bg-foreground/12")}
          />
        ))}
      </span>
      <span className="text-xs">{Math.round(value)}/4</span>
    </span>
  )
}
```

- [ ] **Step 6: Verify typecheck**

Run: `cd apps/saga-web && npx tsc --noEmit`
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add apps/saga-web/src/lib/meters.ts apps/saga-web/src/lib/meters.test.ts apps/saga-web/src/components/meter.tsx
git commit -m "feat(saga-web): speed/precision meter mark"
```

---

## Task 6: shadcn Sheet + Tooltip wrappers

**Files:**
- Create: `apps/saga-web/src/components/ui/sheet.tsx`
- Create: `apps/saga-web/src/components/ui/tooltip.tsx`

**Interfaces:**
- Produces: `Sheet`, `SheetContent`, `SheetHeader`, `SheetTitle`, `SheetClose` (radix `Dialog` under the hood, right-side slide); `TooltipProvider`, `Tooltip`, `TooltipTrigger`, `TooltipContent`.

**Note:** the existing `ui/select.tsx` imports radix as `import { Select as SelectPrimitive } from "radix-ui"`. Follow that exact import style (named export off the `radix-ui` meta-package), not `@radix-ui/react-*`.

- [ ] **Step 1: Create the Sheet wrapper**

Create `apps/saga-web/src/components/ui/sheet.tsx`:

```tsx
import { Dialog as SheetPrimitive } from "radix-ui"
import { X } from "lucide-react"
import type { ComponentProps } from "react"
import { cn } from "@/lib/utils"

export const Sheet = SheetPrimitive.Root
export const SheetTrigger = SheetPrimitive.Trigger
export const SheetClose = SheetPrimitive.Close

export function SheetContent({ className, children, ...props }: ComponentProps<typeof SheetPrimitive.Content>) {
  return (
    <SheetPrimitive.Portal>
      <SheetPrimitive.Overlay className="fixed inset-0 z-50 bg-black/40 backdrop-blur-sm data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0" />
      <SheetPrimitive.Content
        className={cn(
          "fixed inset-y-0 right-0 z-50 flex w-full flex-col border-l bg-background shadow-lg sm:max-w-xl",
          "transition ease-in-out data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:duration-200 data-[state=open]:duration-200 data-[state=closed]:slide-out-to-right data-[state=open]:slide-in-from-right",
          className,
        )}
        {...props}
      >
        {children}
        <SheetPrimitive.Close className="absolute right-4 top-4 rounded-sm opacity-70 transition-opacity hover:opacity-100 focus:outline-none focus:ring-2 focus:ring-[var(--brand)]">
          <X className="size-5" />
          <span className="sr-only">Lukk</span>
        </SheetPrimitive.Close>
      </SheetPrimitive.Content>
    </SheetPrimitive.Portal>
  )
}

export function SheetHeader({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("sticky top-0 z-10 flex items-center gap-3 border-b bg-background/95 px-6 py-4 backdrop-blur", className)} {...props} />
}

export function SheetTitle({ className, ...props }: ComponentProps<typeof SheetPrimitive.Title>) {
  return <SheetPrimitive.Title className={cn("min-w-0 flex-1 truncate text-base font-semibold", className)} {...props} />
}

export function SheetDescription({ className, ...props }: ComponentProps<typeof SheetPrimitive.Description>) {
  return <SheetPrimitive.Description className={cn("sr-only", className)} {...props} />
}
```

- [ ] **Step 2: Create the Tooltip wrapper**

Create `apps/saga-web/src/components/ui/tooltip.tsx`:

```tsx
import { Tooltip as TooltipPrimitive } from "radix-ui"
import type { ComponentProps } from "react"
import { cn } from "@/lib/utils"

export const TooltipProvider = TooltipPrimitive.Provider
export const Tooltip = TooltipPrimitive.Root
export const TooltipTrigger = TooltipPrimitive.Trigger

export function TooltipContent({ className, sideOffset = 4, ...props }: ComponentProps<typeof TooltipPrimitive.Content>) {
  return (
    <TooltipPrimitive.Portal>
      <TooltipPrimitive.Content
        sideOffset={sideOffset}
        className={cn(
          "z-50 rounded-md bg-foreground px-2.5 py-1.5 text-xs text-background shadow-md",
          "data-[state=delayed-open]:animate-in data-[state=delayed-open]:fade-in-0",
          className,
        )}
        {...props}
      />
    </TooltipPrimitive.Portal>
  )
}
```

- [ ] **Step 3: Verify typecheck + build**

Run: `cd apps/saga-web && npx tsc --noEmit && npx vite build`
Expected: clean. (If a radix subpath is not exported as assumed, adjust to the shape the installed `radix-ui@1.6.1` provides - verify with `node -e "const r=require('radix-ui'); console.log(Object.keys(r.Dialog))"`.)

- [ ] **Step 4: Commit**

```bash
git add apps/saga-web/src/components/ui/sheet.tsx apps/saga-web/src/components/ui/tooltip.tsx
git commit -m "feat(saga-web): Sheet (drawer) + Tooltip primitives"
```

---

## Task 7: TierToggle

**Files:**
- Create: `apps/saga-web/src/components/tier-toggle.tsx`

**Interfaces:**
- Consumes: `Tier` from `@/lib/catalog`; `Tooltip*` from `@/components/ui/tooltip`
- Produces: `<TierToggle value={tier} onChange={(t: Tier) => void} cloudEnabled={boolean} />` - segmented control (radix `ToggleGroup`, `type="single"`). `Lokal` neutral; `Turbo` amber text + `Zap` icon + faint amber tint when active. One-line subtitle under each chip. When `cloudEnabled` is false, the Turbo segment is `disabled` and wrapped in a Tooltip ("Sky ikke konfigurert").

- [ ] **Step 1: Implement**

Create `apps/saga-web/src/components/tier-toggle.tsx`:

```tsx
import { ToggleGroup as TG } from "radix-ui"
import { Zap } from "lucide-react"
import type { Tier } from "@/lib/catalog"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"

export function TierToggle({
  value,
  onChange,
  cloudEnabled,
}: {
  value: Tier
  onChange: (t: Tier) => void
  cloudEnabled: boolean
}) {
  const turbo = (
    <TG.Item
      value="cloud"
      disabled={!cloudEnabled}
      aria-label="Turbo"
      className={cn(
        "flex flex-1 flex-col items-start gap-0.5 rounded-lg px-4 py-2.5 text-left transition-colors",
        "data-[state=on]:bg-[color-mix(in_oklch,var(--turbo)_14%,transparent)]",
        "disabled:cursor-not-allowed disabled:opacity-40",
      )}
    >
      <span className={cn("flex items-center gap-1.5 text-sm font-medium", value === "cloud" && "text-[var(--turbo)]")}>
        <Zap className="size-3.5" /> Turbo
      </span>
      <span className="text-xs text-muted-foreground">Raskere og skarpere, via sky</span>
    </TG.Item>
  )

  return (
    <TG.Root
      type="single"
      value={value}
      onValueChange={(v) => v && onChange(v as Tier)}
      className="flex gap-1 rounded-xl border bg-muted/40 p-1"
    >
      <TG.Item
        value="local"
        aria-label="Lokal"
        className="flex flex-1 flex-col items-start gap-0.5 rounded-lg px-4 py-2.5 text-left transition-colors data-[state=on]:bg-background data-[state=on]:shadow-sm"
      >
        <span className="text-sm font-medium">Lokal</span>
        <span className="text-xs text-muted-foreground">Gratis, egen maskin</span>
      </TG.Item>
      {cloudEnabled ? (
        turbo
      ) : (
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="flex-1">{turbo}</span>
          </TooltipTrigger>
          <TooltipContent>Sky ikke konfigurert</TooltipContent>
        </Tooltip>
      )}
    </TG.Root>
  )
}
```

- [ ] **Step 2: Verify typecheck**

Run: `cd apps/saga-web && npx tsc --noEmit`
Expected: clean. (Confirm `ToggleGroup` is exported off `radix-ui`; verified present in Task setup.)

- [ ] **Step 3: Commit**

```bash
git add apps/saga-web/src/components/tier-toggle.tsx
git commit -m "feat(saga-web): Lokal/Turbo tier toggle"
```

---

## Task 8: ModelPicker (Advanced popover)

**Files:**
- Create: `apps/saga-web/src/components/model-picker.tsx`

**Interfaces:**
- Consumes: `Model`, `Tier`, `byTier` from `@/lib/catalog`; `<Meter>`; `Popover` from `radix-ui`
- Produces: `<ModelPicker models={Model[]} value={modelId} onChange={(id: string) => void} />` - an `Avansert ▾` trigger opening a Popover. Catalog grouped `Lokal` / `Sky`. Each row: 3 zones - name (+ `Cloud` glyph for cloud + "Anbefalt" badge when `default`) · truncated `note` (full in `title`) · two right-aligned `<Meter>` (speed, precision), column-aligned. Selected row: `bg-accent` + brand left border. One-time legend line atop the list ("Fart / Presisjon, 1-4").

- [ ] **Step 1: Implement**

Create `apps/saga-web/src/components/model-picker.tsx`:

```tsx
import { Popover } from "radix-ui"
import { ChevronDown, Cloud } from "lucide-react"
import type { Model } from "@/lib/catalog"
import { byTier } from "@/lib/catalog"
import { Meter } from "@/components/meter"
import { cn } from "@/lib/utils"

function Row({ m, selected, onPick }: { m: Model; selected: boolean; onPick: () => void }) {
  return (
    <button
      type="button"
      onClick={onPick}
      className={cn(
        "flex w-full items-center gap-3 rounded-md border-l-2 border-transparent px-3 py-2 text-left transition-colors hover:bg-accent",
        selected && "border-l-[var(--brand)] bg-accent",
      )}
    >
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="flex items-center gap-1.5 text-sm font-medium">
          {m.tier === "cloud" && <Cloud className="size-3 text-muted-foreground" aria-hidden />}
          {m.label}
          {m.default && (
            <span className="rounded-full bg-[color-mix(in_oklch,var(--brand)_18%,transparent)] px-1.5 py-0.5 text-[10px] font-medium text-[var(--brand)]">
              Anbefalt
            </span>
          )}
        </span>
        <span className="truncate text-xs text-muted-foreground" title={m.note}>
          {m.note}
        </span>
      </span>
      <span className="flex shrink-0 flex-col items-end gap-1">
        <Meter kind="speed" value={m.speed} />
        <Meter kind="precision" value={m.precision} />
      </span>
    </button>
  )
}

export function ModelPicker({
  models,
  value,
  onChange,
}: {
  models: Model[]
  value: string
  onChange: (id: string) => void
}) {
  const groups: { key: string; label: string; rows: Model[] }[] = [
    { key: "local", label: "Lokal", rows: byTier(models, "local") },
    { key: "cloud", label: "Sky", rows: byTier(models, "cloud") },
  ]
  return (
    <Popover.Root>
      <Popover.Trigger className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
        Avansert <ChevronDown className="size-3.5" />
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          align="start"
          sideOffset={6}
          className="z-50 w-[22rem] rounded-xl border bg-popover p-2 shadow-lg"
        >
          <p className="px-3 pb-1 pt-1 text-xs text-muted-foreground">Fart / Presisjon (1-4)</p>
          {groups.map((g) => (
            <div key={g.key} className="mb-1">
              <p className="px-3 py-1 text-xs font-semibold uppercase tracking-wide text-muted-foreground">{g.label}</p>
              {g.rows.map((m) => (
                <Popover.Close asChild key={m.id}>
                  <Row m={m} selected={m.id === value} onPick={() => onChange(m.id)} />
                </Popover.Close>
              ))}
            </div>
          ))}
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  )
}
```

**Note:** `bg-popover` is used above; if the theme lacks `--popover`, use `bg-background` instead. Verify against `styles.css` (Task 4 did not add `--popover`) - use `bg-background border` to be safe. Implementer: prefer `bg-background`.

- [ ] **Step 2: Verify typecheck**

Run: `cd apps/saga-web && npx tsc --noEmit`
Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add apps/saga-web/src/components/model-picker.tsx
git commit -m "feat(saga-web): Advanced model picker with meters"
```

---

## Task 9: Composer (hero form)

**Files:**
- Create: `apps/saga-web/src/components/composer.tsx`

**Interfaces:**
- Consumes: `Model`, `Tier`, `tierDefault` from `@/lib/catalog`; `resolveSelection`, `loadStored`, `saveSelection` from `@/lib/selection`; `<TierToggle>`, `<ModelPicker>`; `Job`, `NewJobResponse` from `@/types`
- Produces: `<Composer models={Model[]} cloudEnabled={boolean} onOptimistic={(job: Job) => void} />`. Renders the hero: large URL input with a docked submit icon button, tier toggle under it, language + Avansert as tertiary controls. On submit: POST `/api/jobs`, on success call `onOptimistic` with a synthesized `queued` Job (from returned `id` + pasted URL), clear + refocus the input. NO navigation. Switching tier snaps `modelId` to that tier's default; both persist to localStorage.

**Optimistic Job shape:** `{ id, module: "yt-summary", input: { url, lang, model }, status: "queued", attempts: 0, progress: "", error: null, created_at: new Date().toISOString(), video_title: null }`.

- [ ] **Step 1: Implement**

Create `apps/saga-web/src/components/composer.tsx`:

```tsx
import { useEffect, useRef, useState } from "react"
import { ArrowRight } from "lucide-react"
import type { Model, Tier } from "@/lib/catalog"
import { tierDefault } from "@/lib/catalog"
import { loadStored, resolveSelection, saveSelection } from "@/lib/selection"
import { TierToggle } from "@/components/tier-toggle"
import { ModelPicker } from "@/components/model-picker"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import type { Job, NewJobResponse } from "@/types"

export function Composer({
  models,
  cloudEnabled,
  onOptimistic,
}: {
  models: Model[]
  cloudEnabled: boolean
  onOptimistic: (job: Job) => void
}) {
  const [url, setUrl] = useState("")
  const [lang, setLang] = useState<"no" | "en">("en")
  const [sel, setSel] = useState(() => resolveSelection(models, null))
  const [submitting, setSubmitting] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  // Reconcile stored selection against the live catalog once, on mount (client
  // only - localStorage is unavailable during SSR).
  useEffect(() => {
    setSel(resolveSelection(models, loadStored()))
  }, [models])

  function setTier(tier: Tier) {
    const next = { tier, modelId: tierDefault(models, tier)?.id ?? "" }
    setSel(next)
    saveSelection(next)
  }

  function setModel(modelId: string) {
    const m = models.find((x) => x.id === modelId)
    const next = { tier: m?.tier ?? sel.tier, modelId }
    setSel(next)
    saveSelection(next)
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setSubmitting(true)
    setErr(null)
    try {
      const res = await fetch("/api/jobs", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ module: "yt-summary", input: { url, lang, model: sel.modelId } }),
      })
      if (!res.ok) throw new Error(`saga-api returned ${res.status}`)
      const { id } = (await res.json()) as NewJobResponse
      onOptimistic({
        id,
        module: "yt-summary",
        input: { url, lang, model: sel.modelId },
        status: "queued",
        attempts: 0,
        progress: "",
        error: null,
        created_at: new Date().toISOString(),
        video_title: null,
      })
      setUrl("")
      inputRef.current?.focus()
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setSubmitting(false)
    }
  }

  const selectedLabel = models.find((m) => m.id === sel.modelId)?.label ?? sel.modelId

  return (
    <form onSubmit={submit} className="mx-auto flex max-w-2xl flex-col items-center gap-4">
      <div className="relative w-full">
        <input
          ref={inputRef}
          type="url"
          required
          placeholder="Lim inn en YouTube-URL"
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          className="h-14 w-full rounded-xl border bg-background pl-5 pr-14 text-lg outline-none focus:ring-2 focus:ring-[var(--brand)]"
        />
        <button
          type="submit"
          disabled={submitting}
          aria-label="Oppsummer"
          className="absolute right-2 top-2 grid size-10 place-items-center rounded-lg bg-[var(--brand)] text-white transition-opacity disabled:opacity-50"
        >
          <ArrowRight className="size-5" />
        </button>
      </div>

      <div className="w-full">
        <TierToggle value={sel.tier} onChange={setTier} cloudEnabled={cloudEnabled} />
      </div>

      <div className="flex w-full items-center justify-between gap-3 text-sm">
        <Select value={lang} onValueChange={(v) => setLang(v as "no" | "en")}>
          <SelectTrigger className="h-8 w-32"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="en">English</SelectItem>
            <SelectItem value="no">Norsk</SelectItem>
          </SelectContent>
        </Select>
        <span className="text-muted-foreground">
          Modell: <span className="text-foreground">{selectedLabel}</span>
        </span>
        <ModelPicker models={models} value={sel.modelId} onChange={setModel} />
      </div>

      {err && <p className="text-sm text-destructive">{err}</p>}
    </form>
  )
}
```

- [ ] **Step 2: Verify typecheck**

Run: `cd apps/saga-web && npx tsc --noEmit`
Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add apps/saga-web/src/components/composer.tsx
git commit -m "feat(saga-web): hero composer with tier toggle + optimistic submit"
```

---

## Task 10: JobCard (grid card, per-state)

**Files:**
- Create: `apps/saga-web/src/components/job-card.tsx`

**Interfaces:**
- Consumes: `Job` from `@/types`; `videoId`, `thumbUrl` from `@/lib/youtube`; `<StatusPill>` from `@/ui`; `Model` from `@/lib/catalog` (for the "laget med" label lookup)
- Produces: `<JobCard job={Job} models={Model[]} onOpen={() => void} highlighted={boolean} />` - 16:9 thumbnail top (YouTube thumb when derivable, neutral placeholder otherwise), `line-clamp-2` title, author/url line, footer status pill + per-state affordance. `queued`: "I kø" + spinner. `running`: progress + ETA text + thin progress bar under thumbnail + pulsing dot. `failed`: error snippet (Retry lives in the drawer, not the card, to keep the card a pure open-target - see note). `done`: duration + "laget med `<label>` - `<tier-label>`". Hover elevates (shadow), not fill. `highlighted` adds a brand ring (the just-submitted optimistic card).

**Note:** spec §4.5 places Retry on the failed card. Keeping Retry in the drawer (Task 11) is a simplification to avoid a nested interactive control inside the card's click target (the card is a button that opens the drawer). Flag this to the controller: **plan-vs-spec conflict** - §4.5 says card-level Retry. Controller decides. Default here: Retry in drawer only; the failed card shows the error snippet and opens the drawer where Retry lives.

- [ ] **Step 1: Implement**

Create `apps/saga-web/src/components/job-card.tsx`:

```tsx
import { Loader2 } from "lucide-react"
import type { Job } from "@/types"
import type { Model } from "@/lib/catalog"
import { videoId, thumbUrl } from "@/lib/youtube"
import { StatusPill } from "@/ui"
import { cn } from "@/lib/utils"

function modelLabel(job: Job, models: Model[]): string | null {
  const id = typeof job.input.model === "string" ? job.input.model : null
  if (!id) return null
  const m = models.find((x) => x.id === id)
  if (!m) return id
  return `${m.label} - ${m.tier === "cloud" ? "Turbo" : "Lokal"}`
}

export function JobCard({
  job,
  models,
  onOpen,
  highlighted,
}: {
  job: Job
  models: Model[]
  onOpen: () => void
  highlighted?: boolean
}) {
  const rawUrl = typeof job.input.url === "string" ? job.input.url : ""
  const vid = rawUrl ? videoId(rawUrl) : null
  const title = job.video_title || rawUrl || `job ${job.id}`

  return (
    <button
      type="button"
      onClick={onOpen}
      className={cn(
        "group flex flex-col overflow-hidden rounded-xl border bg-card text-left transition-shadow hover:shadow-md",
        highlighted && "ring-2 ring-[var(--brand)]",
      )}
    >
      <div className="relative aspect-video w-full overflow-hidden bg-muted">
        {vid ? (
          <img src={thumbUrl(vid)} alt="" className="size-full object-cover" />
        ) : (
          <div className="grid size-full place-items-center text-muted-foreground">YouTube</div>
        )}
        {job.status === "running" && (
          <div className="absolute inset-x-0 bottom-0 h-1 overflow-hidden bg-black/20">
            <div className="h-full w-1/3 animate-pulse bg-[var(--status-running)]" />
          </div>
        )}
      </div>

      <div className="flex flex-1 flex-col gap-2 p-4">
        <p className="line-clamp-2 font-medium leading-snug">{title}</p>
        <div className="mt-auto flex items-center gap-2 text-xs text-muted-foreground">
          <StatusPill status={job.status} />
          {job.status === "queued" && <span className="inline-flex items-center gap-1"><Loader2 className="size-3 animate-spin" /> I kø</span>}
          {job.status === "running" && <span className="tabular-nums">{job.progress || "kjører"}</span>}
          {job.status === "failed" && <span className="line-clamp-1 text-destructive">{job.error ?? "feilet"}</span>}
          {job.status === "done" && modelLabel(job, models) && <span className="tabular-nums">laget med {modelLabel(job, models)}</span>}
        </div>
      </div>
    </button>
  )
}
```

- [ ] **Step 2: Verify typecheck**

Run: `cd apps/saga-web && npx tsc --noEmit`
Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add apps/saga-web/src/components/job-card.tsx
git commit -m "feat(saga-web): per-state job card for the dashboard grid"
```

---

## Task 11: Route restructure - layout, dashboard, drawer

**Files:**
- Create: `apps/saga-web/src/routes/_dash.tsx`
- Create: `apps/saga-web/src/routes/_dash.index.tsx`
- Create: `apps/saga-web/src/routes/_dash.jobs.$id.tsx`
- Delete: `apps/saga-web/src/routes/index.tsx`
- Delete: `apps/saga-web/src/routes/jobs.$id.tsx`

**Interfaces:**
- Consumes: everything above. `fetchModels` (client), `listJobs`/`getJob` server fns, `<Composer>`, `<JobCard>`, `<Markdown>`, `<VideoCard>`, `Sheet*`, `TooltipProvider`.
- Produces: the running app. `_dash.tsx` (pathless layout) owns the always-mounted dashboard (hero when empty, grid when jobs exist) + 5s poll + optimistic insert + filter chips, and renders `<Outlet/>` for the drawer. `_dash.index.tsx` (`/`) renders nothing extra (drawer closed). `_dash.jobs.$id.tsx` (`/jobs/$id`) renders the `<Sheet>` open for that id and owns the SSE lifecycle; closing navigates to `/`.

**Architecture note (why this shape):** a pathless layout route (`_dash`) renders the dashboard once and keeps it mounted across `/` and `/jobs/$id`. The drawer is the layout's child `<Outlet/>`. This satisfies spec §4.4: one component owns the drawer + SSE, the dashboard renders behind it even on cold deep-load, opening pushes history (`/jobs/$id`), and Back lands on `/` without remounting/refetching the dashboard.

- [ ] **Step 1: Create the layout with dashboard**

Create `apps/saga-web/src/routes/_dash.tsx`:

```tsx
import { createFileRoute, Outlet, useNavigate } from "@tanstack/react-router"
import { createServerFn } from "@tanstack/react-start"
import { useEffect, useMemo, useState } from "react"
import type { Job, JobStatus } from "../types"
import type { Model } from "@/lib/catalog"
import { fetchModels } from "@/lib/catalog"
import { listJobs } from "../api"
import { Shell } from "../ui"
import { Composer } from "@/components/composer"
import { JobCard } from "@/components/job-card"
import { TooltipProvider } from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"

const fetchJobs = createServerFn({ method: "GET", strict: { output: false } }).handler(
  async (): Promise<Job[]> => listJobs(),
)

export const Route = createFileRoute("/_dash")({
  component: DashLayout,
  loader: () => fetchJobs(),
  errorComponent: () => (
    <Shell>
      <p className="text-destructive">Får ikke kontakt med saga-api.</p>
    </Shell>
  ),
})

type Filter = "all" | JobStatus
const FILTERS: { key: Filter; label: string }[] = [
  { key: "all", label: "Alle" },
  { key: "running", label: "Kjører" },
  { key: "done", label: "Ferdig" },
  { key: "failed", label: "Feilet" },
]

function DashLayout() {
  const loaded = Route.useLoaderData()
  const navigate = useNavigate()

  const [jobs, setJobs] = useState<Job[]>(loaded)
  const [optimistic, setOptimistic] = useState<Job[]>([])
  const [highlightId, setHighlightId] = useState<number | null>(null)
  const [models, setModels] = useState<Model[]>([])
  const [cloudEnabled, setCloudEnabled] = useState(true)
  const [filter, setFilter] = useState<Filter>("all")
  const [q, setQ] = useState("")

  // Server loader reruns on router.invalidate; keep local state in sync and
  // drop optimistic rows the server now knows about.
  useEffect(() => {
    setJobs(loaded)
    setOptimistic((opt) => opt.filter((o) => !loaded.some((j) => j.id === o.id)))
  }, [loaded])

  useEffect(() => {
    fetchModels().then((r) => {
      setModels(r.models)
      setCloudEnabled(r.cloudEnabled)
    }).catch(() => {})
  }, [])

  useEffect(() => {
    const t = setInterval(() => Route.router?.invalidate?.() ?? navigate({ to: ".", replace: true }), 5_000)
    return () => clearInterval(t)
  }, [navigate])

  const merged = useMemo(() => {
    const seen = new Set(jobs.map((j) => j.id))
    return [...optimistic.filter((o) => !seen.has(o.id)), ...jobs]
  }, [jobs, optimistic])

  const shown = useMemo(() => {
    const needle = q.trim().toLowerCase()
    return merged.filter((j) => {
      if (filter !== "all" && j.status !== filter) return false
      if (!needle) return true
      const t = (j.video_title || "") + " " + (typeof j.input.url === "string" ? j.input.url : "")
      return t.toLowerCase().includes(needle)
    })
  }, [merged, filter, q])

  const hasJobs = merged.length > 0

  return (
    <TooltipProvider delayDuration={200}>
      <Shell>
        <section className={cn("transition-all", hasJobs ? "py-6" : "py-20")}>
          <Composer
            models={models}
            cloudEnabled={cloudEnabled}
            onOptimistic={(job) => {
              setOptimistic((o) => [job, ...o])
              setHighlightId(job.id)
            }}
          />
        </section>

        {hasJobs && (
          <>
            <div className="mb-4 flex flex-wrap items-center gap-2">
              {FILTERS.map((f) => (
                <button
                  key={f.key}
                  type="button"
                  onClick={() => setFilter(f.key)}
                  className={cn(
                    "rounded-full border px-3 py-1 text-sm transition-colors",
                    filter === f.key ? "border-[var(--brand)] bg-accent" : "text-muted-foreground hover:bg-accent",
                  )}
                >
                  {f.label}
                </button>
              ))}
              <input
                type="text"
                placeholder="Filtrer på tittel"
                value={q}
                onChange={(e) => setQ(e.target.value)}
                className="ml-auto h-8 w-48 rounded-md border bg-background px-3 text-sm outline-none focus:ring-2 focus:ring-[var(--brand)]"
              />
            </div>

            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              {shown.map((j) => (
                <JobCard
                  key={j.id}
                  job={j}
                  models={models}
                  highlighted={j.id === highlightId}
                  onOpen={() => navigate({ to: "/jobs/$id", params: { id: String(j.id) } })}
                />
              ))}
            </div>
            {shown.length === 0 && <p className="text-muted-foreground">Ingen jobber matcher filteret.</p>}
          </>
        )}
      </Shell>

      <Outlet />
    </TooltipProvider>
  )
}
```

**Implementer note on the poll:** the existing code polled with `router.invalidate()` from `useRouter()`. Use that same pattern here - import `useRouter` and call `router.invalidate()` in the interval (drop the `Route.router?.invalidate?.()` placeholder above, which is not a real API). Match `index.tsx`'s original poll exactly.

- [ ] **Step 2: Create the index leaf**

Create `apps/saga-web/src/routes/_dash.index.tsx`:

```tsx
import { createFileRoute } from "@tanstack/react-router"

// The dashboard lives in the _dash layout; the index leaf renders nothing
// (drawer closed). Deep routes (/jobs/$id) render the drawer via the layout's
// Outlet while this same layout keeps the dashboard mounted behind it.
export const Route = createFileRoute("/_dash/")({
  component: () => null,
})
```

- [ ] **Step 3: Create the drawer route**

Create `apps/saga-web/src/routes/_dash.jobs.$id.tsx` - move the SSE + summary logic from the old `jobs.$id.tsx` into a `<Sheet>`. The drawer owns SSE; closing navigates to `/`:

```tsx
import { createFileRoute, useNavigate } from "@tanstack/react-router"
import { createServerFn } from "@tanstack/react-start"
import { useEffect, useRef, useState } from "react"
import type { Job, ProgressEvent } from "../types"
import { getJob } from "../api"
import { StatusPill } from "../ui"
import { Markdown } from "../markdown"
import { estimateEta } from "@/lib/eta"
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetDescription } from "@/components/ui/sheet"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { ExternalLink, Copy, Download } from "lucide-react"

const fetchJob = createServerFn({ method: "GET", strict: { output: false } })
  .validator((id: unknown): number => Number(id))
  .handler(async ({ data: id }): Promise<Job | null> => getJob(id))

export const Route = createFileRoute("/_dash/jobs/$id")({
  component: JobDrawer,
  loader: ({ params }) => fetchJob({ data: Number(params.id) }),
})

function isTerminal(s: Job["status"]): boolean {
  return s === "done" || s === "failed"
}

async function getJobClient(id: number): Promise<Job | null> {
  const res = await fetch(`/api/jobs/${id}`)
  if (res.status === 404) return null
  if (!res.ok) throw new Error(`saga-api returned ${res.status}`)
  return (await res.json()) as Job
}

function JobDrawer() {
  const initial = Route.useLoaderData()
  const { id } = Route.useParams()
  const navigate = useNavigate()

  const [job, setJob] = useState<Job | null>(initial)
  const [live, setLive] = useState("")
  const [tokens, setTokens] = useState("")
  const tokensRef = useRef("")
  const [eta, setEta] = useState<string | null>(null)
  const chunkTiming = useRef<{ start: number; startIdx: number }>({ start: 0, startIdx: 0 })
  const [streamKey, setStreamKey] = useState(0)
  const jobRef = useRef(job)
  jobRef.current = job

  useEffect(() => {
    const cur = jobRef.current
    if (!cur || isTerminal(cur.status)) return
    tokensRef.current = ""
    setTokens("")
    chunkTiming.current = { start: 0, startIdx: 0 }
    setEta(null)
    const es = new EventSource(`/api/events?job=${id}`)
    let snapshotSeen = false
    es.onmessage = (e) => {
      const data = JSON.parse(e.data) as Job | ProgressEvent
      if (!snapshotSeen && "status" in data) {
        snapshotSeen = true
        setJob(data as Job)
        return
      }
      const ev = data as ProgressEvent
      if (ev.token) {
        tokensRef.current += ev.token
        setTokens(tokensRef.current)
      } else if (ev.stage) {
        setLive(ev.detail ? `${ev.stage}: ${ev.detail}` : ev.stage)
        const m = ev.detail?.match(/chunk (\d+)\/(\d+)/)
        if (m) {
          const i = Number(m[1]), n = Number(m[2]), now = Date.now()
          if (chunkTiming.current.start === 0) chunkTiming.current = { start: now, startIdx: i }
          else setEta(estimateEta(chunkTiming.current.start, now, i - chunkTiming.current.startIdx, n - i))
        }
      }
      if (ev.stage === "done" || ev.stage === "failed") {
        es.close()
        getJobClient(Number(id)).then((j) => {
          if (!j) return
          setJob(j)
          if (!isTerminal(j.status)) setStreamKey((k) => k + 1)
        })
      }
    }
    es.onerror = () => es.close()
    return () => es.close()
  }, [id, streamKey])

  function close() {
    navigate({ to: "/" })
  }

  const rawUrl = job && typeof job.input.url === "string" ? job.input.url : null
  const safeHref = rawUrl && /^https?:\/\//i.test(rawUrl) ? rawUrl : undefined
  const title = job?.video_title || rawUrl || `job ${id}`

  return (
    <Sheet open onOpenChange={(o) => !o && close()}>
      <SheetContent>
        <SheetHeader>
          {job && <StatusPill status={job.status} />}
          <SheetTitle>{title}</SheetTitle>
          <SheetDescription>Jobbdetaljer</SheetDescription>
        </SheetHeader>

        <div className="flex-1 overflow-y-auto px-6 py-5">
          {!job ? (
            <p className="text-destructive">Fant ikke jobb {id}.</p>
          ) : (
            <>
              <div className="mb-4 flex flex-wrap gap-2">
                {safeHref && (
                  <a href={safeHref} target="_blank" rel="noreferrer">
                    <Button variant="outline" size="sm"><ExternalLink className="size-4" /> Åpne på YouTube</Button>
                  </a>
                )}
              </div>

              {!isTerminal(job.status) && (
                <div className="mb-4">
                  <p className="text-muted-foreground">
                    {live || job.progress || job.status}{eta ? ` - ${eta} igjen` : ""}
                  </p>
                  {tokens && <pre className="mt-2 whitespace-pre-wrap rounded-lg bg-muted p-3 text-sm">{tokens}</pre>}
                </div>
              )}

              {job.status === "failed" && <FailedView job={job} onRetried={() => setStreamKey((k) => k + 1)} setJob={setJob} setLive={setLive} />}
              {job.status === "done" && job.result_markdown && <SummaryView job={job} />}
            </>
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}

function FailedView({
  job,
  onRetried,
  setJob,
  setLive,
}: {
  job: Job
  onRetried: () => void
  setJob: (j: Job) => void
  setLive: (s: string) => void
}) {
  const [busy, setBusy] = useState(false)
  return (
    <div className="mb-4">
      <p className="text-destructive">{job.error ?? "feilet"}</p>
      <Button
        className="mt-2"
        size="sm"
        disabled={busy}
        onClick={async () => {
          setBusy(true)
          const res = await fetch(`/api/jobs/${job.id}/retry`, { method: "POST" })
          if (res.ok) {
            const j = await getJobClient(job.id)
            if (j) {
              setLive("")
              setJob(j)
              onRetried()
            }
          }
          setBusy(false)
        }}
      >
        {busy ? "Prøver igjen..." : "Prøv igjen"}
      </Button>
    </div>
  )
}

function SummaryView({ job }: { job: Job }) {
  const [showNo, setShowNo] = useState(false)
  const [translated, setTranslated] = useState<string | null>(job.translated_markdown ?? null)
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  async function toNorwegian() {
    if (translated) { setShowNo(true); return }
    setErr(null)
    setLoading(true)
    try {
      const res = await fetch(`/api/jobs/${job.id}/translate`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ lang: "no" }),
      })
      if (!res.ok) throw new Error(`oversetting feilet: ${res.status}`)
      const { translated_markdown } = (await res.json()) as { translated_markdown: string }
      setTranslated(translated_markdown)
      setShowNo(true)
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }

  const body = showNo && translated ? translated : job.result_markdown!
  return (
    <div>
      <div className="mb-4 flex flex-wrap gap-2">
        <Button variant={showNo ? "outline" : "default"} size="sm" onClick={() => setShowNo(false)}>English</Button>
        <Button variant={showNo ? "default" : "outline"} size="sm" onClick={toNorwegian} disabled={loading}>
          {loading ? "Oversetter..." : "Norsk"}
        </Button>
        <Button variant="outline" size="sm" onClick={() => navigator.clipboard?.writeText(body)}>
          <Copy className="size-4" /> Kopier
        </Button>
        <Button
          variant="outline"
          size="sm"
          onClick={() => {
            const blob = new Blob([body], { type: "text/markdown" })
            const a = document.createElement("a")
            a.href = URL.createObjectURL(blob)
            a.download = `saga-${job.id}.md`
            a.click()
            URL.revokeObjectURL(a.href)
          }}
        >
          <Download className="size-4" /> Last ned
        </Button>
      </div>
      {err && <p className="text-sm text-destructive">{err}</p>}
      {loading ? (
        <div className="space-y-2">
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-5/6" />
          <Skeleton className="h-4 w-2/3" />
        </div>
      ) : (
        <Markdown source={body} />
      )}
    </div>
  )
}
```

- [ ] **Step 4: Delete the old routes**

```bash
git rm apps/saga-web/src/routes/index.tsx apps/saga-web/src/routes/jobs.\$id.tsx
```

- [ ] **Step 5: Regenerate route tree + verify**

Run: `cd apps/saga-web && npx vite build`
Expected: `routeTree.gen.ts` regenerates with `_dash`, `_dash/`, `_dash/jobs/$id`; build succeeds. If TanStack complains about the pathless layout naming, confirm the file is `_dash.tsx` (underscore prefix = pathless) and children are `_dash.index.tsx` / `_dash.jobs.$id.tsx`.

- [ ] **Step 6: Typecheck**

Run: `cd apps/saga-web && npx tsc --noEmit`
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add apps/saga-web/src/routes/
git commit -m "feat(saga-web): dashboard layout + job drawer (no forced navigation)"
```

---

## Task 12: Backend `cloud_enabled` in /api/models + E2E verification

**Files:**
- Modify: `apps/saga-api/internal/api/server.go` - `/api/models` returns `cloud_enabled`
- Modify: `apps/saga-api/internal/api/server_test.go` - assert the field is present

**Interfaces:**
- Consumes: whether the router has a cloud provider. The server already knows this (router built with/without the cloud key at boot). Expose it as a bool on the API server struct (e.g. `s.cloudEnabled`) set in `main.go` from `cfg.OllamaAPIKey != ""`, and include it in the `/api/models` response body as `cloud_enabled`.
- Produces: `GET /api/models` -> `{ "models": [...], "cloud_enabled": bool }`, consumed by `fetchModels` (Task 2).

**Implementer note:** find how the server struct is constructed in `server.go` + `main.go`. Add a `CloudEnabled bool` to the server's config/struct, set it in `main.go` (`cfg.OllamaAPIKey != ""`), and thread it into the `/api/models` handler. Keep the existing `catalog.All()` payload under `models`.

- [ ] **Step 1: Write the failing test**

In `server_test.go`, extend `TestGetModels` (or add a sibling) to decode the body and assert both keys:

```go
	var body struct {
		Models       []map[string]any `json:"models"`
		CloudEnabled bool             `json:"cloud_enabled"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Models) == 0 {
		t.Error("expected models in payload")
	}
	// testServer wires no cloud key -> cloud_enabled must be false
	if body.CloudEnabled {
		t.Error("cloud_enabled should be false when no key configured")
	}
```

Ensure `testServer` constructs the server with cloud disabled (no key). If `testServer` doesn't currently set this, wire the new field to `false` there.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/saga-api && go test ./internal/api/ -run TestGetModels -count=1`
Expected: FAIL (no `cloud_enabled` in payload / field unknown).

- [ ] **Step 3: Implement**

Thread `cloudEnabled` into the server and the handler:

```go
	mux.HandleFunc("GET /api/models", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"models":        catalog.All(),
			"cloud_enabled": s.cloudEnabled,
		})
	})
```

Add the field to the server struct + constructor + set it in `main.go` from `cfg.OllamaAPIKey != ""`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd apps/saga-api && go test ./internal/api/ -count=1 && go build ./...`
Expected: PASS + build OK.

- [ ] **Step 5: Commit**

```bash
git add apps/saga-api/internal/api/ apps/saga-api/main.go
git commit -m "feat(saga-api): expose cloud_enabled on /api/models for the Turbo gate"
```

- [ ] **Step 6: E2E - build + Playwright smoke (local, against a running stack)**

This step is a manual/agent E2E gate, not an automated unit test. Bring up saga-api (with a test DB + a local or tailnet Ollama, cloud key optional) and saga-web `npm run build && npm start` behind the `/api` split, then drive Playwright:

Verify, picky about pixels (per Svein's standard):
1. First visit (no jobs): centered hero, large URL input, tier toggle under it, `Avansert` opens the picker with speed/precision meters grouped Lokal/Sky, "Anbefalt" on the two defaults.
2. Turbo segment: amber when active; if cloud key unset, disabled with "Sky ikke konfigurert" tooltip.
3. Submit a URL: an optimistic `queued` card appears at the front of the grid, input clears + refocuses, NO navigation, card is brand-ringed. Next poll reconciles it to the real job.
4. Click a card: drawer slides in from the right, URL becomes `/jobs/$id`, dashboard visible behind the `bg-black/40 backdrop-blur` overlay. Escape / Back / close-X all close and land on `/` with focus returned.
5. Cold deep-load `/jobs/$id`: dashboard renders behind the open drawer.
6. `done` job in the drawer: English/Norsk toggle, Kopier, Last ned, Åpne på YouTube all work; Norwegian toggle shows the translating skeleton then renders.
7. Light + dark: brand teal + amber turbo read correctly in both; status pills legible.
8. Non-existent id deep-load: drawer shows "Fant ikke jobb", dashboard behind, close to `/`.

Fix any pixel/interaction issues found before marking complete. Capture a couple of screenshots for the report.

- [ ] **Step 7: Commit any E2E fixes**

```bash
git add -A && git commit -m "fix(saga-web): address Phase 2a E2E findings"
```

---

## Self-Review Notes (controller)

- **Plan-vs-spec conflict (Task 10):** §4.5 puts Retry on the failed card; this plan puts Retry in the drawer only (the card is a single click-target that opens the drawer). Batch this to Svein with the spec text before Task 10.
- **`cloud_enabled` (Task 12) is a spec gap:** §4.3 requires the Turbo gate to reflect server key state, but `/api/models` didn't expose it. Task 12 adds it; it is sequenced last only because the frontend defaults `cloudEnabled` to `true` and degrades gracefully, but it could run first if preferred.
- **Poll mechanism:** reuse the exact `useRouter().invalidate()` pattern from the old `index.tsx`; the `_dash.tsx` sketch has a placeholder to replace.
- **No Compare view, no pairwise backend** - that is Phase 2b.
