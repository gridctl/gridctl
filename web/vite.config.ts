import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  base: '/',
  build: {
    chunkSizeWarningLimit: 1000,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (['react', 'react-dom', 'react-router-dom'].some(pkg => id.includes(`/node_modules/${pkg}/`))) {
            return 'vendor-react'
          }
          if (['@xyflow/react', '@dagrejs/dagre'].some(pkg => id.includes(`/node_modules/${pkg}/`))) {
            return 'vendor-graph'
          }
          if (id.includes('/node_modules/recharts/')) {
            return 'vendor-charts'
          }
        },
      },
    },
  },
})
