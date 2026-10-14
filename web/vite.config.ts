import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: {
    host: "127.0.0.1",
    port: 19329,
    strictPort: true,
    cors: false,
    hmr: {
      host: "127.0.0.1",
      clientPort: Number(process.env.CVTRANSLATE_HTTP_PORT || 19328),
    },
  },
  build: { outDir: "dist", emptyOutDir: true },
});
