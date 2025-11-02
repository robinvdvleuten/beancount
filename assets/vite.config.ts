import * as path from "node:path";
import { defineConfig } from "vite";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react(), tailwindcss()],

  server: {
    proxy: {
      "/api": "http://localhost:8080",
    },
  },

  build: {
    outDir: path.resolve(__dirname, "../web/dist"),
    emptyOutDir: true,
    manifest: true,
    rollupOptions: {
      // Specify entry point (not index.html)
      input: "./src/main.tsx",
    },
  },
});
