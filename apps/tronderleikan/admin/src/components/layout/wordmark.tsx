// Ordmerke for admin-planet: samme fjell/fjord-glyf som web, men med en tydelig
// "Admin"-etikett så det aldri forveksles med den tenant-vendte appen.
export function Wordmark() {
  return (
    <span className="flex items-center gap-2">
      <svg
        viewBox="0 0 24 24"
        className="text-primary size-6"
        fill="none"
        aria-hidden="true"
      >
        <path
          d="M2 20 L9 7 L13 14 L16 9 L22 20 Z"
          fill="currentColor"
          fillOpacity="0.18"
        />
        <path
          d="M2 20 L9 7 L13 14 L16 9 L22 20"
          stroke="currentColor"
          strokeWidth="1.75"
          strokeLinejoin="round"
          strokeLinecap="round"
        />
      </svg>
      <span className="flex items-center gap-2">
        <span className="text-[15px] leading-none font-bold tracking-tight">
          Trønder<span className="text-primary">Leikan</span>
        </span>
        <span className="bg-primary/10 text-primary ring-primary/20 rounded px-1.5 py-0.5 text-[10px] font-semibold tracking-wide uppercase ring-1 ring-inset">
          Admin
        </span>
      </span>
    </span>
  )
}
