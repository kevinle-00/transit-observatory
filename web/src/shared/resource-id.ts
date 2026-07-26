export function hasControlCharacters(value: string) {
  return Array.from(value).some((character) => {
    const code = character.codePointAt(0) ?? 0
    return code <= 31 || code >= 127 && code <= 159
  })
}

export function isValidResourceId(value: string | null) {
  if (!value || value.trim().length === 0 || new TextEncoder().encode(value).length > 256) return false
  return !hasControlCharacters(value)
}
