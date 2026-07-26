import { describe, expect, it } from 'vitest'
import { parseApiBaseUrl } from './env'

describe('parseApiBaseUrl', () => {
  it('defaults in development and strips one or more trailing slashes', () => {
    expect(parseApiBaseUrl(undefined, true)).toBe('http://localhost:8080')
    expect(parseApiBaseUrl('https://api.example.test///', false)).toBe('https://api.example.test')
  })

  it('rejects absent production and unsafe URLs', () => {
    expect(() => parseApiBaseUrl(undefined, false)).toThrow('required')
    expect(() => parseApiBaseUrl('file:///tmp/api', false)).toThrow('HTTP or HTTPS')
    expect(() => parseApiBaseUrl('https://user:pass@example.test', false)).toThrow('credentials')
  })
})
