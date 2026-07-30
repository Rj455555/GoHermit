import { act, screen } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'

beforeEach(() => {
  vi.resetModules()
  document.body.innerHTML = '<div id="root"></div>'
})

it('mounts the React bootstrap into the document root', async () => {
  await act(async () => {
    await import('./main')
  })

  expect(screen.getByTestId('react-bootstrap')).toBeInTheDocument()
})
