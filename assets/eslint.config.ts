import js from "@eslint/js";
import { defineConfig } from "eslint/config";
import prettier from "eslint-config-prettier/flat";
import solid from "eslint-plugin-solid/configs/typescript";
import tseslint from "typescript-eslint";

export default defineConfig([
  {
    ignores: [".vite/**/*", "node_modules/**/*"],
  },
  js.configs.recommended,
  tseslint.configs.recommendedTypeChecked,
  prettier,
  // solid plugin has incompatible types with flat config
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  solid as any,
  {
    languageOptions: {
      parserOptions: {
        projectService: true,
      },
    },
  },
]);
