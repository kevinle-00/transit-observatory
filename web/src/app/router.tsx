import { createBrowserRouter, type RouteObject } from 'react-router-dom'
import { AlertDetailPage } from '../alerts/AlertDetailPage'
import { AlertHistoryPage } from '../alerts/AlertHistoryPage'
import { AnalyticsOverviewPage } from '../analytics/AnalyticsOverviewPage'
import { LineAnalyticsPage } from '../analytics/LineAnalyticsPage'
import { Dashboard } from '../dashboard/Dashboard'
import { LineDetailPage, LineExplorerPage } from '../lines'
import { StationDetailPage, StationExplorerPage } from '../stations'
import { AppShell } from './AppShell'
import { NotFound } from './NotFound'
import { RouteError } from './RouteError'

export const routes: RouteObject[] = [{
  path: '/',
  element: <AppShell />,
  errorElement: <RouteError />,
  children: [
    { index: true, element: <Dashboard /> },
    { path: 'lines', element: <LineExplorerPage /> },
    { path: 'lines/detail', element: <LineDetailPage /> },
    { path: 'stations', element: <StationExplorerPage /> },
    { path: 'stations/detail', element: <StationDetailPage /> },
    { path: 'alerts', element: <AlertHistoryPage /> },
    { path: 'alerts/:alertId', element: <AlertDetailPage /> },
    { path: 'analytics', element: <AnalyticsOverviewPage /> },
    { path: 'analytics/line', element: <LineAnalyticsPage /> },
    { path: '*', element: <NotFound /> },
  ],
}]

export const router = createBrowserRouter(routes)
