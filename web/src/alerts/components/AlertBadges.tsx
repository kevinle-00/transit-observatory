import type { AlertRoute } from '../../api/contracts'
import { cssColor } from '../../shared/format'

function routeName(route: AlertRoute) {
  return route.short_name || route.long_name || route.source_route_id
}

export function AlertRouteBadges({ routes }: { routes: AlertRoute[] }) {
  return (
    <div className="alert-feature__badges" aria-label="Source-listed lines">
      {routes.length === 0 && <span className="alert-feature__badge alert-feature__badge--neutral">No lines listed</span>}
      {routes.map((route) => (
        <span className="alert-feature__badge" key={route.source_route_id} style={{ '--alert-line': cssColor(route.color) } as React.CSSProperties}>
          <i aria-hidden="true" />{routeName(route)}{!route.is_matched && <em>unmatched</em>}
        </span>
      ))}
    </div>
  )
}
