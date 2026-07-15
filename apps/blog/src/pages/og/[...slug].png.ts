import type { APIContext } from "astro";
import { getPosts } from "../../lib/posts.ts";
import { renderOgPng } from "../../lib/og.ts";

export async function getStaticPaths() {
  const posts = await getPosts();
  return posts.map((post) => ({
    params: { slug: post.id },
    props: {
      title: post.data.ogTitle ?? post.data.title,
      subtitle: post.data.pubDate.toLocaleDateString("en-US", {
        year: "numeric",
        month: "long",
        day: "numeric",
      }),
    },
  }));
}

export async function GET({ props }: APIContext) {
  const png = await renderOgPng(props as { title: string; subtitle?: string });
  return new Response(new Uint8Array(png), {
    headers: { "Content-Type": "image/png", "Cache-Control": "public, max-age=31536000, immutable" },
  });
}
