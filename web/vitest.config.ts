import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      // CodeMirror can't render under jsdom; swap it for a textarea stub.
      '@uiw/react-codemirror': fileURLToPath(
        new URL('./src/test/codemirrorStub.tsx', import.meta.url),
      ),
      // The @lobehub/icons barrel drags in JSON modules Node ESM can't load
      // under vitest; swap every icon for a plain svg stub.
      '@lobehub/icons': fileURLToPath(
        new URL('./src/test/lobehubStub.tsx', import.meta.url),
      ),
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./vitest.setup.ts'],
  },
})
