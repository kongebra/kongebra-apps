import { defineCollection, z } from "astro:content";
import { glob } from "astro/loaders";

// Colocated posts: src/content/posts/<slug>/index.mdx (+ images next to it).
// generateId strips the folder structure to a clean URL slug (the directory name).
const posts = defineCollection({
  loader: glob({
    pattern: "**/index.{md,mdx}",
    base: "./src/content/posts",
    generateId: ({ entry }) => entry.split("/")[0],
  }),
  schema: ({ image }) =>
    z.object({
      title: z.string(),
      description: z.string(),
      pubDate: z.coerce.date(),
      updatedDate: z.coerce.date().optional(),
      tags: z.array(z.string()).default([]),
      draft: z.boolean().default(false),
      // Colocated raster image -> Astro/Sharp emits hashed responsive webp with width/height.
      heroImage: image().optional(),
      // Alt text for the hero. Required whenever heroImage is set (a11y + image SEO); "" = decorative.
      heroAlt: z.string().optional(),
      // Override the auto-generated OG card title if the post title is too long.
      ogTitle: z.string().optional(),
    })
    .refine((d) => d.heroImage === undefined || d.heroAlt !== undefined, {
      message: "heroAlt is required when heroImage is set (use an empty string only if decorative)",
      path: ["heroAlt"],
    }),
});

export const collections = { posts };
