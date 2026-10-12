import { defineConfig } from "vite";
export default defineConfig({
  root: "ui",
  base: "/", // goxide-lang.github.io is an organization root site.
  oxc: { jsx: { runtime: "automatic", importSource: "preact" } },
  build: { outDir: "../dist", emptyOutDir: true, sourcemap: false },
  worker: { format: "es" },
});
