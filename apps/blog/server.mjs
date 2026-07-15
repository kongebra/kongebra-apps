import { createReadStream } from "node:fs";
import { stat } from "node:fs/promises";
import { createServer } from "node:http";
import { extname, join, normalize } from "node:path";

const root = new URL("./dist/", import.meta.url).pathname;
const port = Number(process.env.PORT ?? 3000);

const types = {
  ".html": "text/html; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".xml": "application/xml; charset=utf-8",
  ".txt": "text/plain; charset=utf-8",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".webp": "image/webp",
  ".avif": "image/avif",
  ".ico": "image/x-icon",
  ".woff": "font/woff",
  ".woff2": "font/woff2",
};

// ponytail: 'unsafe-inline' for script-src covers the two tiny inline theme scripts (pre-paint
// no-flash + toggle). Upgrade path: emit sha256 hashes or a nonce and drop 'unsafe-inline'.
const csp =
  "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; " +
  "img-src 'self' data:; font-src 'self' data:; connect-src 'self'; base-uri 'self'; " +
  "object-src 'none'; form-action 'self'; frame-ancestors 'none'";

const securityHeaders = {
  "x-content-type-options": "nosniff",
  "referrer-policy": "strict-origin-when-cross-origin",
  // Public HTTPS host (TLS terminated at Cloudflare/Traefik). Long max-age, cover subdomains.
  "strict-transport-security": "max-age=63072000; includeSubDomains",
  "content-security-policy": csp,
};

async function isFile(path) {
  try {
    return (await stat(path)).isFile();
  } catch {
    return false;
  }
}

// Resolve a request path to a file on disk. Returns { file, status }.
async function resolve(pathname) {
  const rel = normalize(pathname).replace(/^(\.\.(\/|\\|$))+/, "").replace(/^\//, "");
  const base = join(root, rel);

  // 1. Exact asset (/_astro/..., /rss.xml, /og/x.png, /favicon.svg).
  if (rel && (await isFile(base))) return { file: base, status: 200 };
  // 2. Directory-format page (/posts/slug/ -> .../index.html; also handles "/").
  const index = join(base, "index.html");
  if (await isFile(index)) return { file: index, status: 200 };
  // 3. Flat .html (defensive; Astro uses directory format here).
  if (await isFile(`${base}.html`)) return { file: `${base}.html`, status: 200 };
  // 4. Real 404.
  return { file: join(root, "404.html"), status: 404 };
}

function cacheControl(file) {
  if (file.includes("/_astro/")) return "public, max-age=31536000, immutable";
  if (file.endsWith(".html")) return "no-cache";
  return "public, max-age=3600";
}

const server = createServer(async (request, response) => {
  try {
    if (request.url === "/health") {
      response.writeHead(200, { "content-type": "text/plain; charset=utf-8" });
      response.end("ok\n");
      return;
    }

    // Malformed percent-encoding (e.g. /%ZZ) makes decodeURIComponent throw -> 400, not a hung socket.
    let pathname;
    try {
      pathname = decodeURIComponent(new URL(request.url ?? "/", "http://localhost").pathname);
    } catch {
      response.writeHead(400, { "content-type": "text/plain; charset=utf-8" });
      response.end("bad request\n");
      return;
    }

    const { file, status } = await resolve(pathname);
    response.writeHead(status, {
      "content-type": types[extname(file)] ?? "application/octet-stream",
      "cache-control": cacheControl(file),
      ...securityHeaders,
    });
    createReadStream(file).pipe(response);
  } catch {
    // Never leave a request hanging on an unexpected error.
    if (!response.headersSent) response.writeHead(500, { "content-type": "text/plain; charset=utf-8" });
    response.end("internal error\n");
  }
});

server.listen(port, "0.0.0.0");

// PID 1 in the container: without these, SIGTERM is ignored and every rollout waits out the full
// terminationGracePeriod before SIGKILL. close() drains in-flight requests, then exit clean.
const shutdown = () => server.close(() => process.exit(0));
process.on("SIGTERM", shutdown);
process.on("SIGINT", shutdown);
