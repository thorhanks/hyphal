// Serves the spike with the CSP from CONTRACTS, so the bundle is proven under the real policy.
import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { extname, join, normalize } from "node:path";

const types = { ".html": "text/html; charset=utf-8", ".js": "text/javascript", ".css": "text/css" };
const csp = "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'";
const root = join(import.meta.dirname, "assets");
const port = Number(process.env.PORT ?? 47900);

createServer(async (req, res) => {
  const path = normalize(new URL(req.url, "http://x").pathname).replace(/^\/+/, "") || "index.html";
  try {
    const body = await readFile(join(root, path));
    res.writeHead(200, { "Content-Type": types[extname(path)] ?? "application/octet-stream", "Content-Security-Policy": csp });
    res.end(body);
  } catch {
    res.writeHead(404);
    res.end();
  }
}).listen(port, "127.0.0.1", () => console.log(`http://127.0.0.1:${port}/`));
