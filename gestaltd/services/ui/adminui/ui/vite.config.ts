import { defineConfig } from "vite";
import { gestalt } from "@valon-technologies/gestalt-web/vite";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

const root = dirname(fileURLToPath(import.meta.url));
const apiOrigin = process.env.GESTALT_API_PROXY_TARGET?.trim() || "http://127.0.0.1:8080";

export default defineConfig({
  base: "/admin/",
  plugins: [
    gestalt(),
    tanstackRouter({
      target: "react",
      autoCodeSplitting: true,
      routesDirectory: "./src/routes",
      generatedRouteTree: "./src/routeTree.gen.ts",
    }),
    react(),
    tailwindcss(),
  ],
  resolve: {
    alias: { "@": join(root, "src") },
  },
  build: {
    outDir: "../out",
    emptyOutDir: true,
    rollupOptions: {
      output: {
        entryFileNames: "assets/app-[hash].js",
        chunkFileNames: "assets/[name]-[hash].js",
        assetFileNames: "assets/[name]-[hash][extname]",
      },
    },
  },
  server: {
    host: "127.0.0.1",
    proxy: {
      "/admin/api": { target: apiOrigin.replace(/\/+$/, ""), changeOrigin: true },
      "/api": { target: apiOrigin.replace(/\/+$/, ""), changeOrigin: true },
      "/metrics": { target: apiOrigin.replace(/\/+$/, ""), changeOrigin: true },
    },
  },
});
