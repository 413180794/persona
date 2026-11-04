import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// `npm run dev` talks to a hub through Vite's proxy: JARVIS_HUB_URL (eval "$(python3 ../scripts/jarvis_config.py env)").
const environment = (globalThis as { process?: { env: Record<string, string | undefined> } }).process?.env ?? {};
const hub = (environment.PERSONA_HUB_URL ?? environment.JARVIS_HUB_URL) || "http://127.0.0.1:18660";

// The hub admin UI (docs/admin.md) is its own page, admin/index.html. Its code goes under admin/assets/, which the hub
// serves only to sources inside admin.access; the client's bundle never references it.
const adminOnly = (ids: string[]) => ids.length > 0 && ids.every((id) => id.includes("/src/admin/") || id.endsWith("/admin/index.html"));

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
  // The terminal loads xterm only when opened: pre-bundle it, or the first open in dev re-optimizes and reloads the page.
  optimizeDeps: { include: ["@xterm/xterm", "@xterm/addon-fit"] },
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
