import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [vue(), tailwindcss()],
  build: {
    // Output straight into the Go embed folder.
    outDir: '../server/web/dist',
    emptyOutDir: true,
    target: 'es2022',
  },
  server: {
    proxy: {
      '/api': { target: process.env.NVR_DEV_API ?? 'http://127.0.0.1:8090', ws: true, changeOrigin: false },
    },
  },
})
