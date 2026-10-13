import { Component, type ErrorInfo, type ReactNode } from 'react'
import { Icon } from './Icon'

export default class ErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false }
  static getDerivedStateFromError() { return { failed: true } }
  componentDidCatch(error: Error, info: ErrorInfo) { console.error('M.I.E.R. workspace render failed', error, info.componentStack) }
  render() {
    if (!this.state.failed) return this.props.children
    return <main className="workspace-recovery" role="alert">
      <Icon name="alert-circle" size={32}/><h1>The workspace could not be displayed</h1>
      <p>Reload the workspace to recover. Drafts already saved on this device will be restored.</p>
      <button className="btn btn-primary" onClick={() => location.reload()}><Icon name="refresh"/>Reload workspace</button>
    </main>
  }
}
