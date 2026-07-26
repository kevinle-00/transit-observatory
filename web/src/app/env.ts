const DEFAULT_API_BASE_URL = 'http://localhost:8080'

export function parseApiBaseUrl(value: string | undefined, isDevelopment = import.meta.env.DEV): string {
  const candidate = value?.trim() || (isDevelopment ? DEFAULT_API_BASE_URL : '')
  if (!candidate) {
    throw new Error('VITE_API_BASE_URL is required outside development')
  }

  let parsed: URL
  try {
    parsed = new URL(candidate)
  } catch {
    throw new Error('VITE_API_BASE_URL must be a valid URL')
  }
  if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
    throw new Error('VITE_API_BASE_URL must use HTTP or HTTPS')
  }
  if (parsed.username || parsed.password || parsed.search || parsed.hash) {
    throw new Error('VITE_API_BASE_URL must not include credentials, a query, or a fragment')
  }
  return parsed.toString().replace(/\/+$/, '')
}

export const apiBaseUrl = parseApiBaseUrl(import.meta.env.VITE_API_BASE_URL)
