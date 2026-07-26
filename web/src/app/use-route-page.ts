import { useEffect, type RefObject } from 'react'
import { useLocation } from 'react-router-dom'

export function useRoutePage(title: string, heading: RefObject<HTMLElement | null>) {
  const location = useLocation()

  useEffect(() => {
    document.title = title
  }, [title])

  useEffect(() => {
    heading.current?.focus()
  }, [heading, location.key])
}
