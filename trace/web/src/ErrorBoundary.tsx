import { Component, type ErrorInfo, type ReactNode } from 'react'

type Props = { children: ReactNode }
type State = { error: Error | null }

export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null }

  static getDerivedStateFromError(error: Error) {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('UI crash', error, info.componentStack)
  }

  render() {
    if (this.state.error) {
      return (
        <div className="panel" style={{ margin: '2rem auto', maxWidth: 520 }} role="alert">
          <h2 style={{ marginTop: 0 }}>Something went wrong</h2>
          <p className="meta">{this.state.error.message}</p>
          <div className="row gap">
            <button type="button" className="btn" onClick={() => this.setState({ error: null })}>Try again</button>
            <button type="button" className="btn secondary" onClick={() => { location.href = '/' }}>Go home</button>
          </div>
        </div>
      )
    }
    return this.props.children
  }
}
