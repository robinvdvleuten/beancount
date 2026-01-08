import * as path from "node:path";
import { type Plugin, defineConfig } from "vite";
import tailwindcss from "@tailwindcss/vite";
import solid from "vite-plugin-solid";
import solidSvg from "vite-plugin-solid-svg";

const metadataDevValue = {
  version: "dev",
  commitSHA: "local",
  readOnly: false,
};

const filesDevValue = {
  root: "example.beancount",
  includes: [],
};

// Plugin to handle globals: replaces Go templates in HTML (dev only) and provides virtual module
function globalsPlugin(): Plugin {
  const virtualModuleId = "virtual:globals";
  const resolvedId = "\0" + virtualModuleId;

  return {
    name: "globals",
    resolveId(id) {
      if (id === virtualModuleId) {
        return resolvedId;
      }
    },
    load(id) {
      if (id === resolvedId) {
        if (process.env.NODE_ENV === "development") {
          // Dev mode: return actual values
          return `export const meta = ${JSON.stringify(metadataDevValue)};
export const files = ${JSON.stringify(filesDevValue)};`;
        } else {
          // Production: read from window (set by Go at runtime)
          return `export const meta = window.__metadata;
export const files = window.__files;`;
        }
      }
    },
    transformIndexHtml(html) {
      // In dev server, replace Go template variables with actual values
      if (process.env.NODE_ENV === "development") {
        return html
          .replace(/\{\{ \.Metadata \}\}/g, JSON.stringify(metadataDevValue))
          .replace(/\{\{ \.Files \}\}/g, JSON.stringify(filesDevValue));
      }
    },
  };
}

export default defineConfig({
  plugins: [solid(), solidSvg(), tailwindcss(), globalsPlugin()],

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
