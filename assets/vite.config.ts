import * as path from "node:path";
import { type Plugin, defineConfig } from "vite";
import tailwindcss from "@tailwindcss/vite";
import solid from "vite-plugin-solid";
import solidSvg from "vite-plugin-solid-svg";

// Plugin to replace Go template variable ONLY in dev server mode
// In production build, it stays as-is for Go to replace at runtime
function metadataPlugin(): Plugin {
  return {
    name: 'metadata',
    apply: 'serve', // Only run during dev server, not build
    transformIndexHtml(html) {
      // In dev server, inject complete metadata JSON object
      const metadata = {
        version: 'dev',
        commitSHA: 'local',
        readOnly: false,
      };
      return html.replace(/\{\{ \.Metadata \}\}/g, JSON.stringify(metadata));
    },
  };
}

export default defineConfig({
  plugins: [solid(), solidSvg(), tailwindcss(), metadataPlugin()],

  server: {
    proxy: {
      "/api": "http://localhost:8080",
    },
  },

  build: {
    outDir: path.resolve(__dirname, "../web/dist"),
    emptyOutDir: false,
    manifest: true,
    rollupOptions: {
      input: {
        main: path.resolve(__dirname, "index.html"),
      },
    },
  },
});
