import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Geliştirmede /api, yerel API'ye vekillenir (üretimde nginx aynı işi yapar).
export default defineConfig({
  plugins: [react()],
  server: {
    host: true,
    port: 3000,
    proxy: { '/api': { target: 'http://localhost:8080', rewrite: (p) => p.replace(/^\/api/, '') } },
  },
})
