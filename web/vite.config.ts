import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// `npm run dev` talks to a hub through Vite's proxy: JARVIS_HUB_URL (eval "$(python3 ../scripts/jarvis_config.py env)").
const hub = (globalThis as { process?: { env: Record<string, string | undefined> } }).process?.env.JARVIS_HUB_URL || "http://127.0.0.1:18660";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    host: "127.0.0.1",
    proxy: { "/api": { target: hub, changeOrigin: true, ws: true } },
  },
  build: { target: "es2022", sourcemap: false, chunkSizeWarningLimit: 900 },
});
