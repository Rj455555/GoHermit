import { createReadStream } from 'node:fs'
import { createServer } from 'node:http'
import { extname, resolve } from 'node:path'

const root = resolve('internal/web/assets')
const allowed = new Set(['/index.html', '/app.js', '/styles.css'])
const types = { '.html': 'text/html; charset=utf-8', '.js': 'text/javascript; charset=utf-8', '.css': 'text/css; charset=utf-8' }

createServer((request, response) => {
  const pathname = new URL(request.url || '/', 'http://127.0.0.1').pathname
  const asset = pathname === '/' ? '/index.html' : pathname
  if (!allowed.has(asset)) {
    response.writeHead(404).end('not found')
    return
  }
  response.setHeader('Content-Type', types[extname(asset)])
  createReadStream(resolve(root, asset.slice(1))).pipe(response)
}).listen(4173, '127.0.0.1')
