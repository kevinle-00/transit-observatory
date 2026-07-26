import type { AlertStation } from '../api/contracts'

export function stationNames(stations: AlertStation[]) {
  return stations.map((station) => station.name || station.source_stop_id)
}
