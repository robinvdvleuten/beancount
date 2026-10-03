import * as fs from "node:fs";
import * as path from "node:path";
import { type Plugin, defineConfig } from "vite";
import tailwindcss from "@tailwindcss/vite";
import solid from "vite-plugin-solid";
import solidSvg from "vite-plugin-solid-svg";

const metadataDevValue = {
  version: "dev",
  commitSHA: "local",
  readOnly: false,
  watching: false,
  title: "Beancount",
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
          return `export const meta = ${JSON.stringify(metadataDevValue)};`;
        } else {
          // Production: read from window (set by Go at runtime)
          return `export const meta = window.__metadata;`;
        }
      }
    },
    transformIndexHtml(html) {
      // In dev server, replace Go template variables with actual values
      if (process.env.NODE_ENV === "development") {
        return html.replace(/\{\{ \.Metadata \}\}/g, JSON.stringify(metadataDevValue));
      }
    },
  };
}

const outDir = path.resolve(__dirname, "../web/dist");

// Plugin to write back the .gitignore the build empties out of the output
// directory, which keeps the directory, and nothing in it, in git so the Go
// embed compiles before a build.
function keepOutDirPlugin(): Plugin {
  return {
    name: "keep-out-dir",
    apply: "build",
    writeBundle() {
      fs.writeFileSync(path.join(outDir, ".gitignore"), "*\n!.gitignore\n");
    },
  };
}

export default defineConfig({
  plugins: [solid(), solidSvg(), tailwindcss(), globalsPlugin(), keepOutDirPlugin()],

  server: {
    proxy: {
      "/api": "http://localhost:8080",
    },
  },

  build: {
    outDir,
    // Each build replaces the last, so the binary embeds only its bundles.
    emptyOutDir: true,
    manifest: true,
    rollupOptions: {
      input: {
        main: path.resolve(__dirname, "index.html"),
      },
    },
  },
});
