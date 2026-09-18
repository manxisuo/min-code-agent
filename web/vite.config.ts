import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";

// Relative base so dist/ can be previewed or served from any path.
export default defineConfig({
  plugins: [vue()],
  base: "./",
  build: {
    outDir: "dist",
    emptyOutDir: true,
    assetsDir: "assets",
  },
  server: {
    port: 5173,
    proxy: {
      "/api": "http://127.0.0.1:8080",
    },
  },
});
