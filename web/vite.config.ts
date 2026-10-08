// vitest/config re-exports Vite's defineConfig with the `test` block typed,
// which keeps one config file instead of two that can drift apart.
import { defineConfig } from "vitest/config";
import type { Plugin } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative, sep } from "node:path";
import { availableParallelism, loadavg } from "node:os";
import { fileURLToPath, URL } from "node:url";

// The root VERSION file is the one release.sh bumps, so the client reads it
// rather than keeping a second copy in package.json.
const version = readFileSync(fileURLToPath(new URL("../VERSION", import.meta.url)), "utf8").trim();

// Excalidraw fetches its fonts from window.EXCALIDRAW_ASSET_PATH, and names a
// public CDN beside it as a fallback, which the browser asks for fonts it
// loads before the path is read. The build writes the fonts under that path,
// the dev server serves them there, and the fallback is rewritten to the same
// path, so a sketch never reaches another host.
const excalidrawFonts = fileURLToPath(new URL("./node_modules/@excalidraw/excalidraw/dist/prod/fonts", import.meta.url));
// SKETCH_ASSET_PATH in src/config.ts, which cannot be imported here since it
// reads the build's own constants; sketch.test.ts holds the two together.
const fontsPath = "/assets/excalidraw/fonts/";
const fallback = /`https:\/\/esm\.sh\/\$\{.*?\}\/dist\/prod\/`/;
const ownFallback = `\`\${window.location.origin}${fontsPath.slice(0, -"fonts/".length)}\``;

function filesUnder(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    return statSync(path).isDirectory() ? filesUnder(path) : [path];
  });
}

function excalidrawAssets(): Plugin {
  return {
    name: "stator-excalidraw-assets",
    configureServer(server) {
      server.middlewares.use(fontsPath, (req, res, next) => {
        const path = join(excalidrawFonts, decodeURIComponent((req.url ?? "").split("?")[0] ?? ""));
        if (!path.startsWith(excalidrawFonts + sep) || !path.endsWith(".woff2")) return next();
        try {
          res.setHeader("Content-Type", "font/woff2");
          res.end(readFileSync(path));
        } catch {
          next();
        }
      });
    },
    transform(code, id) {
      if (!id.includes("@excalidraw/excalidraw") || !code.includes("ASSETS_FALLBACK_URL")) return null;
      if (!fallback.test(code))
        this.error("Excalidraw no longer names its font CDN as this build expects; find its ASSETS_FALLBACK_URL and point it at our own fonts again.");
      return { code: code.replace(fallback, ownFallback), map: null };
    },
    generateBundle() {
      for (const path of filesUnder(excalidrawFonts)) {
        const name = relative(excalidrawFonts, path).split(sep).join("/");
        this.emitFile({ type: "asset", fileName: `${fontsPath.slice(1)}${name}`, source: readFileSync(path) });
      }
    },
  };
}

// Gates in parallel worktrees share one machine, and with vitest's default of
// a worker per core three of them starved one another past the test timeout.
// So a run takes at most a share of the cores, and only those not already busy.
const TEST_CORE_SHARE = 0.5;
const TEST_MIN_WORKERS = 2;
const cores = availableParallelism();
const [busyCores = 0] = loadavg();
const idleCores = cores - Math.round(busyCores);
const testWorkers = Math.max(TEST_MIN_WORKERS, Math.min(Math.floor(cores * TEST_CORE_SHARE), idleCores));

export default defineConfig({
  plugins: [react(), tailwindcss(), excalidrawAssets()],
  define: {
    __APP_VERSION__: JSON.stringify(version),
  },
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
  server: {
    host: "0.0.0.0",
    port: 5173,
    // Vite rejects requests whose Host header it does not recognise, which
    // blocks anything reaching the dev server by its compose service name.
    allowedHosts: ["localhost", "127.0.0.1", "web"],
    // The API runs on its own origin in development. Proxying it here means the
    // browser sees one origin, so the session cookie is first-party and no CORS
    // preflight is involved.
    proxy: {
      "/api": {
        target: process.env.VITE_API_PROXY || "http://localhost:8080",
        changeOrigin: true,
        // A page's shared draft is a WebSocket under /api too.
        ws: true,
      },
    },
  },
  test: {
    globals: true,
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    maxWorkers: testWorkers,
  },
});
