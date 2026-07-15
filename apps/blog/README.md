# blog

Static blog for **blog.kongebra.no** - Astro + bun + Tailwind v4 + MDX.
Lightning-fast static output, SEO-complete (sitemap, RSS, JSON-LD, per-post OG cards), zero client JS beyond a tiny theme toggle.

## Develop

```bash
bun install
bun run dev          # http://localhost:4321
bun run build        # -> dist/ (pure static)
bun run preview      # serve the build
bun run typecheck    # astro check
bun test             # unit tests
```

## Write a post

Create `src/content/posts/<slug>/index.mdx`:

```mdx
---
title: "Your title"
description: "One-sentence summary (used in listings, meta, OG card)."
pubDate: 2026-07-15
tags: ["k3s", "homelab"]
draft: false          # draft: true is visible in dev, excluded from the build
# heroImage: ./cover.png   # optional; colocate the file, Astro emits webp
---

Body in Markdown/MDX. Code blocks get Shiki highlighting.
```

- **Images:** colocate raster files in the post folder and reference them relatively. Astro + Sharp emit hashed responsive **webp** with width/height baked in. `// ponytail:` move originals to R2 if the repo bloats.
- **URL:** the folder name is the slug -> `/posts/<slug>/`.
- **OG card:** generated automatically at `/og/<slug>.png` (sharp + SVG, no runtime dependency).

## Deploy

CI (`.github/workflows/blog.yml`) builds one immutable image, pushes to GHCR, and promotes the tag through `kongebra-gitops` (dev -> prod behind the reviewer gate). ArgoCD syncs.
Public exposure is via the shared Cloudflare Tunnel (`expose-public` component on the IngressRoute) - no inbound port is opened.

## Stack notes

- Tailwind v4 via `@tailwindcss/vite` (not `@astrojs/tailwind`).
- Fonts self-hosted through `@fontsource-variable/*` (no CDN).
- Theme: `[data-theme]` on `<html>`, set pre-paint to avoid a flash.
- Served in prod by `server.mjs` on distroless Node (per-path HTML + real 404, health on `/health`).
