/**
 * ErrorBoundary — React 错误边界
 *
 * 捕获子组件渲染错误，防止整个应用白屏。
 */

import { Component, type ReactNode } from 'react'

interface Props {
  children: ReactNode
  fallback?: ReactNode
}

interface State {
  hasError: boolean
  error?: Error
}

export default class ErrorBoundary extends Component<Props, State> {
  state: State = { hasError: false }

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error }
  }

  componentDidCatch(error: Error, info: React.ErrorInfo) {
    console.error('[ErrorBoundary]', error, info.componentStack)
  }

  handleRetry = () => {
    this.setState({ hasError: false, error: undefined })
  }

  render() {
    if (this.state.hasError) {
      if (this.props.fallback) return this.props.fallback
      return (
        <div className="error-fallback">
          <div className="ef-icon">&#128165;</div>
          <div className="ef-title">出错了</div>
          <div className="ef-desc">
            {this.state.error?.message || '页面加载异常，请重试'}
          </div>
          <button className="ef-retry" onClick={this.handleRetry}>
            重新加载
          </button>
        </div>
      )
    }
    return this.props.children
  }
}
