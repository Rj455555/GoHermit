import { Component, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { ErrorState } from './ErrorState'

interface BoundaryProps {
  children: ReactNode
  onRetry?: () => void
  copy: {
    title: string
    description: string
    retry: string
    back: string
  }
}

interface BoundaryState {
  failed: boolean
}

class RouteBoundary extends Component<BoundaryProps, BoundaryState> {
  state: BoundaryState = { failed: false }

  static getDerivedStateFromError(): BoundaryState {
    return { failed: true }
  }

  componentDidCatch(): void {
    // The route fallback intentionally does not expose or log response/error bodies.
  }

  private retry = () => {
    this.setState({ failed: false })
    this.props.onRetry?.()
  }

  render(): ReactNode {
    if (!this.state.failed) return this.props.children
    return (
      <ErrorState
        title={this.props.copy.title}
        description={this.props.copy.description}
        action={
          <div className="state-panel__actions">
            <button type="button" className="button button--primary" onClick={this.retry}>
              {this.props.copy.retry}
            </button>
            <Link className="button button--secondary" to="/dashboard">
              {this.props.copy.back}
            </Link>
          </div>
        }
      />
    )
  }
}

export function RouteErrorBoundary({
  children,
  onRetry,
}: {
  children: ReactNode
  onRetry?: () => void
}) {
  const { t } = useTranslation()
  return (
    <RouteBoundary
      {...(onRetry === undefined ? {} : { onRetry })}
      copy={{
        title: t('errorBoundary.title'),
        description: t('errorBoundary.description'),
        retry: t('actions.retry'),
        back: t('actions.backDashboard'),
      }}
    >
      {children}
    </RouteBoundary>
  )
}
