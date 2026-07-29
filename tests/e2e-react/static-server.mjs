import { createReadStream, mkdtempSync, rmSync, statSync } from 'node:fs'
import { createServer } from 'node:http'
import { extname, resolve } from 'node:path'
import { tmpdir } from 'node:os'
import { spawnSync } from 'node:child_process'

const host = '127.0.0.1'
const port = 4174
const root = resolve('internal/web/assets/dist')
const dialogPrefix = '/__test__/dialog'
const dialogRoot = mkdtempSync(resolve(tmpdir(), 'gohermit-dialog-e2e-'))
const types = {
  '.css': 'text/css; charset=utf-8',
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
}

const dialogBuild = spawnSync(
  'pnpm',
  [
    '--filter',
    '@gohermit/web',
    'exec',
    'vite',
    'build',
    '--config',
    resolve('tests/e2e-react/vite.dialog.config.mjs'),
  ],
  {
    cwd: resolve('.'),
    env: {
      ...process.env,
      NODE_ENV: 'production',
      GOHERMIT_DIALOG_HARNESS_OUT: dialogRoot,
    },
    stdio: 'inherit',
  },
)
if (dialogBuild.status !== 0) {
  rmSync(dialogRoot, { recursive: true, force: true })
  process.exit(dialogBuild.status ?? 1)
}

function existingFile(fileRoot, pathname) {
  const candidate = resolve(fileRoot, `.${pathname}`)
  if (!candidate.startsWith(`${fileRoot}/`)) return null
  try {
    return statSync(candidate).isFile() ? candidate : null
  } catch {
    return null
  }
}

const server = createServer((request, response) => {
  const pathname = new URL(request.url || '/', `http://${host}`).pathname
  if (pathname === '/api' || pathname.startsWith('/api/')) {
    response.writeHead(404).end('not found')
    return
  }
  if (pathname === dialogPrefix || pathname === `${dialogPrefix}/`) {
    response.setHeader('Content-Type', types['.html'])
    createReadStream(resolve(dialogRoot, 'dialog-harness.html')).pipe(response)
    return
  }
  if (pathname.startsWith(`${dialogPrefix}/`)) {
    const dialogAsset = existingFile(dialogRoot, pathname.slice(dialogPrefix.length))
    if (dialogAsset) {
      response.setHeader(
        'Content-Type',
        types[extname(dialogAsset)] || 'application/octet-stream',
      )
      createReadStream(dialogAsset).pipe(response)
      return
    }
    response.writeHead(404).end('not found')
    return
  }
  const asset = existingFile(root, pathname)
  if (asset) {
    response.setHeader('Content-Type', types[extname(asset)] || 'application/octet-stream')
    createReadStream(asset).pipe(response)
    return
  }
  if (extname(pathname)) {
    response.writeHead(404).end('not found')
    return
  }
  response.setHeader('Content-Type', types['.html'])
  createReadStream(resolve(root, 'index.html')).pipe(response)
})

function close() {
  server.close(() => {
    rmSync(dialogRoot, { recursive: true, force: true })
    process.exit(0)
  })
}

process.once('SIGINT', close)
process.once('SIGTERM', close)
process.once('exit', () => rmSync(dialogRoot, { recursive: true, force: true }))
server.listen(port, host)
