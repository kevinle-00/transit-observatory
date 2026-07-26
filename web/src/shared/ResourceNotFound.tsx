import { Link } from 'react-router-dom'

export function ResourceNotFound({ resource, backTo, backLabel }: { resource: string; backTo: string; backLabel: string }) {
  return (
    <section className="resource-not-found" role="status">
      <h2>{resource} not found</h2>
      <p>The requested record does not exist or is no longer available.</p>
      <Link to={backTo}>{backLabel}</Link>
    </section>
  )
}
