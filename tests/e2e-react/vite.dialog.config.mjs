import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'

const harnessRoot = dirname(fileURLToPath(import.meta.url))
const repositoryRoot = resolve(harnessRoot, '../..')
const outDir = process.env.GOHERMIT_DIALOG_HARNESS_OUT

if (!outDir) throw new Error('GOHERMIT_DIALOG_HARNESS_OUT is required')

export default {
  root: harnessRoot,
  base: '/__test__/dialog/',
  resolve: {
    alias: [
      {
        find: 'react-dom',
        replacement: resolve(repositoryRoot, 'web/node_modules/react-dom'),
      },
      {
        find: 'react-router-dom',
        replacement: resolve(repositoryRoot, 'web/node_modules/react-router-dom'),
      },
      {
        find: 'react',
        replacement: resolve(repositoryRoot, 'web/node_modules/react'),
      },
    ],
  },
  esbuild: {
    jsx: 'automatic',
  },
  build: {
    outDir,
    emptyOutDir: true,
    sourcemap: false,
    rollupOptions: {
      input: resolve(harnessRoot, 'dialog-harness.html'),
      onwarn(warning, warn) {
        if (warning.code === 'MODULE_LEVEL_DIRECTIVE') return
        warn(warning)
      },
    },
  },
}
