/// <reference types="vitest/config" />
import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// `npm run dev` talks to a hub through Vite's proxy: JARVIS_HUB_URL (eval "$(python3 ../scripts/jarvis_config.py env)").
const environment = (globalThis as { process?: { env: Record<string, string | undefined> } }).process?.env ?? {};
const hub = (environment.PERSONA_HUB_URL ?? environment.JARVIS_HUB_URL) || "http://127.0.0.1:18660";

// The hub admin UI (docs/admin.md) is its own page, admin/index.html. Its code goes under admin/assets/, which the hub
// serves only to sources inside admin.access; the client's bundle never references it.
// The admin UI's strings (src/i18n/locales/<lang>/admin.ts and its admin-*.json) are admin code too.
const adminStrings = /\/src\/i18n\/locales\/[^/]+\/admin(\.ts|-[\w-]+\.json)$/;
const adminOnly = (ids: string[]) =>
  ids.length > 0 && ids.every((id) => id.includes("/src/admin/") || id.endsWith("/admin/index.html") || adminStrings.test(id));

// Dev only: /admin and its client-side routes get admin/index.html (Vite's own fallback is the root index.html).
const adminFallback: Plugin = {
  name: "admin-fallback",
  apply: "serve",
  configureServer(server) {
    server.middlewares.use((req, _res, next) => {
      const r = req as unknown as { url?: string };
      const [path, query] = (r.url ?? "").split(/\?(.*)/s);
      if ((path === "/admin" || path.startsWith("/admin/")) && !path.split("/").pop()!.includes(".")) {
        r.url = "/admin/index.html" + (query ? `?${query}` : "");
      }
      next();
    });
  },
};

export default defineConfig({
  plugins: [react(), tailwindcss(), adminFallback],
  server: {
    host: "127.0.0.1",
    proxy: { "/api": { target: hub, changeOrigin: true, ws: true } },
  },
  // The bundle's files are relative to the page's <base>, which the hub points at the sub-path a reverse proxy serves
  // it under (docs/web.md「子路径」); at the root that is "/", as before.
  base: "./",
  // The terminal loads xterm only when opened: pre-bundle it, or the first open in dev re-optimizes and reloads the page.
  optimizeDeps: { include: ["@xterm/xterm", "@xterm/addon-fit", "@xterm/addon-clipboard", "@xterm/addon-web-links", "@xterm/addon-search", "@xterm/addon-webgl"] },
  // Tests run in zh-Hans, the source language, so their expectations read like the interface (src/i18n/test-setup.ts).
  // Node 25+ has its own localStorage, undefined without --localstorage-file, and it hides jsdom's: turn it off.
  test: { setupFiles: ["src/i18n/test-setup.ts"], execArgv: ["--no-experimental-webstorage"] },
  build: {
    target: "es2022",
    sourcemap: false,
    chunkSizeWarningLimit: 900,
    rollupOptions: {
      // The service worker (src/sw.ts) is its own entry at a fixed /sw.js: its scope is the folder it's served from.
      input: { main: "index.html", admin: "admin/index.html", sw: "src/sw.ts" },
      output: {
        entryFileNames: (chunk) => (chunk.name === "sw" ? "sw.js" : chunk.name === "admin" ? "admin/assets/[name]-[hash].js" : "assets/[name]-[hash].js"),
        chunkFileNames: (chunk) => (adminOnly(chunk.moduleIds) ? "admin/assets/[name]-[hash].js" : "assets/[name]-[hash].js"),
        assetFileNames: (asset) => ((asset.names ?? []).some((n) => n.startsWith("admin")) ? "admin/assets/[name]-[hash][extname]" : "assets/[name]-[hash][extname]"),
      },
    },
  },
});
