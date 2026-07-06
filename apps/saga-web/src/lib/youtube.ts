// Derive YouTube URLs client-side; no API key, no backend. Metadata (title,
// channel, thumbnail) is fetched from the oEmbed endpoint by the consumer.
export function videoId(url: string): string | null {
  try {
    const u = new URL(url)
    if (u.hostname === "youtu.be") return u.pathname.slice(1) || null
    if (u.hostname.endsWith("youtube.com")) {
      if (u.pathname === "/watch") return u.searchParams.get("v")
      const m = u.pathname.match(/^\/embed\/([^/?]+)/)
      if (m) return m[1]
    }
    return null
  } catch {
    return null
  }
}

export const oembedUrl = (url: string) =>
  `https://www.youtube.com/oembed?url=${encodeURIComponent(url)}&format=json`

// youtube-nocookie reduces tracking on the embed.
export const embedUrl = (id: string) => `https://www.youtube-nocookie.com/embed/${id}`
export const thumbUrl = (id: string) => `https://i.ytimg.com/vi/${id}/hqdefault.jpg`
