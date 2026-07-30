import { Component, type ReactNode } from 'react'

interface BootstrapErrorBoundaryProps {
  children: ReactNode
}

interface BootstrapErrorBoundaryState {
  failed: boolean
}

export class BootstrapErrorBoundary extends Component<
  BootstrapErrorBoundaryProps,
  BootstrapErrorBoundaryState
> {
  state: BootstrapErrorBoundaryState = { failed: false }

  static getDerivedStateFromError(): BootstrapErrorBoundaryState {
    return { failed: true }
  }

  render(): ReactNode {
    if (this.state.failed) {
      return <main role="alert">React bootstrap failed.</main>
    }
    return this.props.children
  }
}
