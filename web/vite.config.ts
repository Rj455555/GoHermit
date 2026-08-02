import { fileURLToPath, URL } from 'node:url'
import { readFileSync } from 'node:fs'

import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

const CSS_CONTRACT_ID = '\0gohermit:styles-contract'
const CSS_SOURCE_PATH = fileURLToPath(new URL('./src/styles.css', import.meta.url))

export default defineConfig({
  plugins: [
    {
      name: 'gohermit-css-contract',
      enforce: 'pre',
      resolveId(id) {
        return id === 'virtual:gohermit-styles-contract' ? CSS_CONTRACT_ID : undefined
      },
      load(id) {
        if (id !== CSS_CONTRACT_ID) return undefined
        return `export default ${JSON.stringify(readFileSync(CSS_SOURCE_PATH, 'utf8'))}`
      },
    },
    react(),
  ],
  build: {
    outDir: fileURLToPath(new URL('../internal/web/assets/dist', import.meta.url)),
    emptyOutDir: true,
    sourcemap: false,
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: './src/test/setup.ts',
    // GitHub's shared runner can starve the three Ant Design-heavy suites when
    // Vitest executes files concurrently, causing actionability waits to hit
    // their timeout despite passing assertions. Keep local feedback parallel,
    // but make CI scheduling deterministic without weakening test semantics.
    fileParallelism: !process.env.CI,
    testTimeout: process.env.CI ? 15_000 : 5_000,
    coverage: {
      provider: 'v8',
      reporter: ['text'],
      thresholds: {
        lines: 80,
        functions: 80,
        branches: 80,
        statements: 80,
      },
    },
  },
})
