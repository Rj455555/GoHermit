import { render, screen } from '@testing-library/react'

import { App } from './App'

describe('App', () => {
  it('renders the minimal React bootstrap', () => {
    render(<App />)

    expect(screen.getByTestId('react-bootstrap')).toHaveTextContent(
      'GoHermit React bootstrap is ready.',
    )
  })
})
