import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  base: '/',
  build: {
    // Default warning limit is 500 kB. JsonValueEditor (CodeMirror) is the
    // largest remaining chunk at about 427 kB, so the default holds.
    rolldownOptions: {
      output: {
        codeSplitting: {
          groups: [
            {
              name: 'vendor-react',
              test: /node_modules[\\/](react-dom|react-router|react|scheduler)[\\/]/,
              priority: 30,
              includeDependenciesRecursively: false,
            },
            {
              name: 'vendor-graph',
              test: /node_modules[\\/](@xyflow|@dagrejs)[\\/]/,
              priority: 20,
              includeDependenciesRecursively: false,
            },
            {
              name: 'vendor-charts',
              test: /node_modules[\\/](recharts|victory-vendor|d3-[^/\\]+)[\\/]/,
              priority: 20,
              includeDependenciesRecursively: false,
            },
          ],
        },
      },
    },
  },
})
