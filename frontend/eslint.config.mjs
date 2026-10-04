import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,
  // Ignore build output at any depth. A top-level ".next/**" misses nested folders such as frontend/.next.
  globalIgnores([
    "**/.next/**",
    "**/out/**",
    "**/node_modules/**",
    "**/dist/**",
    "**/build/**",
    "next-env.d.ts",
  ]),
]);

export default eslintConfig;
