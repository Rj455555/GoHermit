import { render, screen } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'

import { BootstrapErrorBoundary } from './BootstrapErrorBoundary'

function ThrowBootstrapError(): never {
  throw new Error('bootstrap test failure')
}

beforeEach(() => {
  vi.spyOn(console, 'error').mockImplementation(() => undefined)
})

it('renders a bounded fallback when bootstrap rendering fails', () => {
  render(
    <BootstrapErrorBoundary>
      <ThrowBootstrapError />
    </BootstrapErrorBoundary>,
  )

  expect(screen.getByRole('alert')).toHaveTextContent('React bootstrap failed.')
})
