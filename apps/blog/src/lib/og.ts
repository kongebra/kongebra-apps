import sharp from "sharp";
import { SITE } from "../consts.ts";

const W = 1200;
const H = 630;

function escapeXml(s: string): string {
  return s.replace(/[<>&'"]/g, (c) =>
    ({ "<": "&lt;", ">": "&gt;", "&": "&amp;", "'": "&apos;", '"': "&quot;" })[c]!,
  );
}

// Naive greedy word-wrap by character budget (OG titles are short; ~18 chars/line at 64px).
function wrap(text: string, maxChars = 18, maxLines = 4): string[] {
  const words = text.split(/\s+/);
  const lines: string[] = [];
  let line = "";
  for (const word of words) {
    if ((line + " " + word).trim().length > maxChars && line) {
      lines.push(line);
      line = word;
    } else {
      line = (line + " " + word).trim();
    }
  }
  if (line) lines.push(line);
  if (lines.length > maxLines) {
    lines.length = maxLines;
    lines[maxLines - 1] = lines[maxLines - 1].replace(/.{1}$/, "…");
  }
  return lines;
}

interface OgInput {
  title: string;
  subtitle?: string;
}

/** Render a 1200x630 OG card to PNG. Rasterized by sharp (librsvg + system fonts at build). */
export async function renderOgPng({ title, subtitle }: OgInput): Promise<Buffer> {
  const lines = wrap(title);
  const lineHeight = 78;
  const startY = H / 2 - ((lines.length - 1) * lineHeight) / 2 - 30;
  const tspans = lines
    .map(
      (l, i) =>
        `<tspan x="80" y="${startY + i * lineHeight}">${escapeXml(l)}</tspan>`,
    )
    .join("");

  const svg = `<svg width="${W}" height="${H}" viewBox="0 0 ${W} ${H}" xmlns="http://www.w3.org/2000/svg">
    <rect width="${W}" height="${H}" fill="#0e1113"/>
    <rect width="${W}" height="6" fill="#3fd9ae"/>
    <text x="80" y="90" font-family="'Schibsted Grotesk','Helvetica Neue',Arial,sans-serif" font-size="30" font-weight="600" fill="#3fd9ae">&#9670; ${escapeXml(SITE.title)}</text>
    <text font-family="'Schibsted Grotesk','Helvetica Neue',Arial,sans-serif" font-size="64" font-weight="700" fill="#e8e6e0" letter-spacing="-1">${tspans}</text>
    <text x="80" y="${H - 60}" font-family="'JetBrains Mono','DejaVu Sans Mono',monospace" font-size="26" fill="#9a988f">${escapeXml(subtitle ?? SITE.tagline)}</text>
  </svg>`;

  return sharp(Buffer.from(svg)).png().toBuffer();
}
