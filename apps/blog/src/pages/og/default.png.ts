import { renderOgPng } from "../../lib/og.ts";
import { SITE } from "../../consts.ts";

// Fallback OG card for the home page and any page without a post-specific card.
export async function GET() {
  const png = await renderOgPng({ title: SITE.tagline, subtitle: SITE.url.replace("https://", "") });
  return new Response(new Uint8Array(png), {
    headers: { "Content-Type": "image/png", "Cache-Control": "public, max-age=31536000, immutable" },
  });
}
