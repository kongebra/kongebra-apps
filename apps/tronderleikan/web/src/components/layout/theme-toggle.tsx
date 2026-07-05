import { useSyncExternalStore } from 'react'
import { Moon, Sun } from 'lucide-react'

import { Button } from '@/components/ui/button'

const THEME_EVENT = 'tl:themechange'

// Leser .dark-klassen reaktivt uten setState-i-effect. Server-snapshot = false;
// FOUC håndteres av inline-scriptet i __root som setter klassen før paint.
function useIsDark(): boolean {
  return useSyncExternalStore(
    (onChange) => {
      window.addEventListener(THEME_EVENT, onChange)
      return () => window.removeEventListener(THEME_EVENT, onChange)
    },
    () => document.documentElement.classList.contains('dark'),
    () => false,
  )
}

export function ThemeToggle() {
  const dark = useIsDark()

  function toggle() {
    const next = !document.documentElement.classList.contains('dark')
    document.documentElement.classList.toggle('dark', next)
    try {
      localStorage.setItem('theme', next ? 'dark' : 'light')
    } catch {
      // localStorage kan være utilgjengelig (privat modus) - ignorer.
    }
    window.dispatchEvent(new Event(THEME_EVENT))
  }

  return (
    <Button
      variant="ghost"
      size="icon"
      onClick={toggle}
      aria-label="Bytt mellom lyst og mørkt tema"
    >
      {dark ? <Sun className="size-4" /> : <Moon className="size-4" />}
    </Button>
  )
}
