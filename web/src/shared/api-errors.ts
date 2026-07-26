import { ApiError } from '../api/client'

export function isNotFoundError(error: unknown) {
  return error instanceof ApiError && error.status === 404
}
