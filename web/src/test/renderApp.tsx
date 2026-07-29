import { act, render } from '@testing-library/react'
import { MemoryRouter, useNavigate, type NavigateFunction } from 'react-router-dom'

import { App } from '../App'

export function renderApp(path = '/dashboard') {
  let navigate: NavigateFunction | undefined
  function Harness() {
    navigate = useNavigate()
    return <App />
  }
  const result = render(
    <MemoryRouter initialEntries={[path]}>
      <Harness />
    </MemoryRouter>,
  )
  return {
    ...result,
    async navigate(pathname: string) {
      await act(() => navigate?.(pathname))
    },
  }
}
