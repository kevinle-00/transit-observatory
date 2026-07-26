import { useEffect, type RefObject } from 'react'

export function useRoutePage(title: string, heading: RefObject<HTMLElement | null>) {
  useEffect(() => {
    document.title = title
    heading.current?.focus()
  }, [heading, title])
}
