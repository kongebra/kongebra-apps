import { createContext, useContext, useEffect, useState, type ReactNode } from "react"

type Theme = "light" | "dark"
const ThemeCtx = createContext<{ theme: Theme; toggle: () => void }>({ theme: "light", toggle: () => {} })

// Runs in <head> before hydration so the .dark class is set pre-paint (no flash).
// Kept as a string injected via dangerouslySetInnerHTML in __root.
export const themeInitScript = `
(function(){try{var t=localStorage.getItem('saga-theme');
if(!t){t=window.matchMedia('(prefers-color-scheme: dark)').matches?'dark':'light';}
if(t==='dark'){document.documentElement.classList.add('dark');}}catch(e){}})();
`

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setTheme] = useState<Theme>("light")
  useEffect(() => {
    // sync React state to whatever the pre-hydration script decided
    setTheme(document.documentElement.classList.contains("dark") ? "dark" : "light")
  }, [])
  const toggle = () => {
    setTheme((prev) => {
      const next = prev === "dark" ? "light" : "dark"
      document.documentElement.classList.toggle("dark", next === "dark")
      try {
        localStorage.setItem("saga-theme", next)
      } catch {}
      return next
    })
  }
  return <ThemeCtx.Provider value={{ theme, toggle }}>{children}</ThemeCtx.Provider>
}

export const useTheme = () => useContext(ThemeCtx)
