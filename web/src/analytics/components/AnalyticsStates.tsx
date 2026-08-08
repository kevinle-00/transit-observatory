import { ErrorState, LoadingState } from '../../shared/QueryState'

export function AnalyticsLoading({ label }: { label: string }) {
  return <div className="analytics-state"><LoadingState label={label} /></div>
}

export function AnalyticsError({ cached = false, onRetry }: { cached?: boolean; onRetry: () => void }) {
  return <div className="analytics-state"><ErrorState cached={cached} title={cached ? 'Analytics could not be refreshed' : 'Analytics could not be loaded'} message={cached ? 'Showing the last available results.' : 'Try again in a moment.'} onRetry={onRetry} /></div>
}
