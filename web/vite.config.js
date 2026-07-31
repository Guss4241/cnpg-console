import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// La SPA est buildée vers internal/web/dist et embarquée dans le binaire Go.
export default defineConfig({
  plugins: [vue()],
  base: '/',
  build: {
    outDir: '../internal/web/dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': 'http://127.0.0.1:8080',
      '/healthz': 'http://127.0.0.1:8080',
    },
  },
})
