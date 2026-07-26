interface LoadingStateProps {
  label: string
  compact?: boolean
}

export function LoadingState({ label, compact = false }: LoadingStateProps) {
  return (
    <div className={`loading-state ${compact ? 'loading-state--compact' : ''}`} role="status" aria-live="polite">
      <span className="loading-state__line" aria-hidden="true" />
      <span>{label}</span>
    </div>
  )
}

interface ErrorStateProps {
  title: string
  message: string
  onRetry: () => void
  cached?: boolean
}

export function ErrorState({ title, message, onRetry, cached = false }: ErrorStateProps) {
  return (
    <div className={`error-state ${cached ? 'error-state--cached' : ''}`} role="alert">
      <div>
        <strong>{title}</strong>
        <p>{message}</p>
      </div>
      <button type="button" className="text-button" onClick={onRetry}>Try again</button>
    </div>
  )
}
