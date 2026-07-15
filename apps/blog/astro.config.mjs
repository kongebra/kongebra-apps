// @ts-check
import { readdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { defineConfig } from "astro/config";
import mdx from "@astrojs/mdx";
import sitemap from "@astrojs/sitemap";
import tailwindcss from "@tailwindcss/vite";

import { SITE } from "./src/consts.ts";

// Map "/posts/<slug>/" -> ISO lastmod (updatedDate ?? pubDate), read straight from frontmatter
// so the sitemap carries a real freshness signal. Cheap scan, runs once at config load.
const postsDir = fileURLToPath(new URL("./src/content/posts", import.meta.url));
const lastmod = Object.fromEntries(
  readdirSync(postsDir, { withFileTypes: true })
    .filter((d) => d.isDirectory())
    .flatMap((d) => {
      try {
        const fm = readFileSync(`${postsDir}/${d.name}/index.mdx`, "utf8").split("---")[1] ?? "";
        const date = (/updatedDate:\s*(.+)/.exec(fm)?.[1] ?? /pubDate:\s*(.+)/.exec(fm)?.[1] ?? "")
          .trim()
          .replace(/['"]/g, "");
        return date ? [[`/posts/${d.name}/`, new Date(date).toISOString()]] : [];
      } catch {
        return [];
      }
    }),
);

// https://astro.build/config
export default defineConfig({
  site: SITE.url,
  // Directory-format URLs (/posts/slug/) -> clean, trailing-slash canonical.
  trailingSlash: "always",
  integrations: [
    mdx(),
    // Drafts are excluded from getCollection() below, so they never reach the sitemap.
    sitemap({
      serialize(item) {
        const lm = lastmod[new URL(item.url).pathname];
        if (lm) item.lastmod = lm;
        return item;
      },
    }),
  ],
  markdown: {
    shikiConfig: {
      // Dual themes: Shiki emits CSS variables, we flip them with [data-theme] (zero client JS).
      themes: { light: "github-light", dark: "github-dark" },
      wrap: false,
    },
  },
  vite: {
    plugins: [tailwindcss()],
  },
});
