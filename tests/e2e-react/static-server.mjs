import { createReadStream, statSync } from 'node:fs'
import { createServer } from 'node:http'
import { extname, resolve } from 'node:path'

const host = '127.0.0.1'
const port = 4174
const root = resolve('internal/web/assets/dist')
const types = {
  '.css': 'text/css; charset=utf-8',
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
}

function existingFile(pathname) {
  const candidate = resolve(root, `.${pathname}`)
  if (!candidate.startsWith(`${root}/`)) return null
  try {
    return statSync(candidate).isFile() ? candidate : null
  } catch {
    return null
  }
}

createServer((request, response) => {
  const pathname = new URL(request.url || '/', `http://${host}`).pathname
  if (pathname === '/api' || pathname.startsWith('/api/')) {
    response.writeHead(404).end('not found')
    return
  }
  const asset = existingFile(pathname)
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
}).listen(port, host)
