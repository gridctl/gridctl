import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  base: './',
  build: {
    // Suppress chunk size warning for embedded CLI tool UI.
    // React Flow library exceeds default 500 kB threshold.
    chunkSizeWarningLimit: 600,
  },
})
