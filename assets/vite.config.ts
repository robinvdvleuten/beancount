import * as path from "node:path";
import { type Plugin, defineConfig } from "vite";
import tailwindcss from "@tailwindcss/vite";
import solid from "vite-plugin-solid";
import solidSvg from "vite-plugin-solid-svg";

const metadataDevValue = {
  version: 'dev',
  commitSHA: 'local',
  readOnly: false,
};

// Plugin to handle metadata: replaces Go template in HTML (dev only) and provides virtual module
function metadataPlugin(): Plugin {
  const virtualModuleId = 'virtual:globals';
  const resolvedId = '\0' + virtualModuleId;

  return {
    name: 'metadata',
    resolveId(id) {
      if (id === virtualModuleId) {
        return resolvedId;
      }
    },
    load(id) {
      if (id === resolvedId) {
        if (process.env.NODE_ENV === 'development') {
          // Dev mode: return actual metadata
          return `export const meta = ${JSON.stringify(metadataDevValue)};`;
        } else {
          // Production: read from window (set by Go at runtime)
          return `export const meta = window.__metadata;`;
        }
      }
    },
    transformIndexHtml(html) {
      // In dev server, replace Go template variable with actual metadata
      if (process.env.NODE_ENV === 'development') {
        return html.replace(/\{\{ \.Metadata \}\}/g, JSON.stringify(metadataDevValue));
      }
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
