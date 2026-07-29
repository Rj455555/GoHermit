import { Component, type ErrorInfo, type ReactNode } from 'react'

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

  componentDidCatch(error: Error, info: ErrorInfo): void {
    console.error('React bootstrap render failed', error, info)
  }

  render(): ReactNode {
    if (this.state.failed) {
      return <main role="alert">React bootstrap failed.</main>
    }
    return this.props.children
  }
}
