import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Backend URL - aibox admin server
const backendUrl = 'http://localhost:8080'

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': {
        target: backendUrl,
        changeOrigin: true,
      }
    }
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  }
})
