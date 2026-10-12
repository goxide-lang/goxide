import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
export default defineConfig({
  root: "ui",
  base: "/", // goxide-lang.github.io is an organization root site.
  plugins: [react()],
  build: { outDir: "../dist", emptyOutDir: true, sourcemap: false },
  worker: { format: "es" },
});
