// Ordmerke: et lite fjell/fjord-glyf i primærfarge + navnet. Bevisst enkelt og
// gjenkjennelig - ikke generisk AI-boilerplate.
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
      <span className="text-[15px] leading-none font-bold tracking-tight">
        Trønder<span className="text-primary">Leikan</span>
      </span>
    </span>
  )
}
