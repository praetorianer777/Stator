// vitest/config re-exports Vite's defineConfig with the `test` block typed,
// which keeps one config file instead of two that can drift apart.
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { readFileSync } from "node:fs";
import { availableParallelism, loadavg } from "node:os";
import { fileURLToPath, URL } from "node:url";

// The root VERSION file is the one release.sh bumps, so the client reads it
// rather than keeping a second copy in package.json.
const version = readFileSync(fileURLToPath(new URL("../VERSION", import.meta.url)), "utf8").trim();

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
  plugins: [react(), tailwindcss()],
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
