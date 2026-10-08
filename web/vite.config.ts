import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The build output is embedded into the Go binary (internal/webui/dist), so a
// single container image carries the whole service for an air-gapped install.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
    sourcemap: false,
    chunkSizeWarningLimit: 1200,
  },
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://127.0.0.1:18088',
      '/auth': 'http://127.0.0.1:18088',
      '/mcp': 'http://127.0.0.1:18088',
      '/healthz': 'http://127.0.0.1:18088',
      '/oauth': 'http://127.0.0.1:18088',
    },
  },
})
