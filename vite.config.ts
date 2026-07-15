// Thin root config so the Lovable harness (which runs `vite` from the
// project root) can build/serve the React app that actually lives in
// ./client. Real config still lives at client/vite.config.ts and is used
// when developing directly from inside client/.
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath } from "node:url";

export default defineConfig({
  root: "client",
  plugins: [react()],
  resolve: {
    alias: { "@": fileURLToPath(new URL("./client/src", import.meta.url)) },
  },
  build: { outDir: "../dist", emptyOutDir: true },
  server: {
    host: true,
    port: 8080,
    proxy: { "/api": { target: "http://127.0.0.1:8081", changeOrigin: true } },
  },
});
