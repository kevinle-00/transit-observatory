import { createBrowserRouter } from 'react-router-dom'
import { Dashboard } from '../dashboard/Dashboard'
import { NotFound } from './NotFound'
import { RouteError } from './RouteError'

export const router = createBrowserRouter([
  { path: '/', element: <Dashboard />, errorElement: <RouteError /> },
  { path: '*', element: <NotFound />, errorElement: <RouteError /> },
])
