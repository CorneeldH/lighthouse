/// <reference types="vitest/config" />
import { fileURLToPath, URL } from 'node:url'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  build: {
    // The Go binary embeds this directory (internal/web: go:embed all:dist).
    outDir: '../internal/web/dist',
    // emptyOutDir removes the tracked dist/.gitkeep; public/.gitkeep puts it back.
    emptyOutDir: true,
  },
  server: {
    proxy: {
      // changeOrigin false: the browser Origin must equal Host, else the write endpoints answer 403.
      '/api': { target: 'http://127.0.0.1:8127', changeOrigin: false },
    },
  },
  test: {
    environment: 'jsdom',
  },
})
