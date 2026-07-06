import { useEffect, useState } from "react"
import { videoId, oembedUrl, embedUrl, thumbUrl } from "@/lib/youtube"
import { Card } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"

type Meta = { title: string; author_name: string }

export function VideoCard({ url }: { url: string }) {
  const id = videoId(url)
  const [meta, setMeta] = useState<Meta | null>(null)
  const [playing, setPlaying] = useState(false)

  useEffect(() => {
    if (!id) return
    let live = true
    fetch(oembedUrl(url))
      .then((r) => (r.ok ? r.json() : null))
      .then((m) => live && m && setMeta({ title: m.title, author_name: m.author_name }))
      .catch(() => {})
    return () => {
      live = false
    }
  }, [id, url])

  if (!id) return null // non-YouTube URL: caller falls back to the markdown title

  return (
    <Card className="mb-6 overflow-hidden p-0">
      {playing ? (
        <div className="aspect-video">
          <iframe
            className="size-full"
            src={`${embedUrl(id)}?autoplay=1`}
            title={meta?.title ?? "video"}
            loading="lazy"
            allow="accelerated-encoded-media; autoplay; encrypted-media; picture-in-picture"
            allowFullScreen
          />
        </div>
      ) : (
        <button
          type="button"
          onClick={() => setPlaying(true)}
          className="group relative block aspect-video w-full"
          aria-label="Play video"
        >
          <img src={thumbUrl(id)} alt={meta?.title ?? "thumbnail"} className="size-full object-cover" />
          <span className="absolute inset-0 grid place-items-center bg-black/20 transition-colors group-hover:bg-black/40">
            <span className="grid size-14 place-items-center rounded-full bg-black/70 text-2xl text-white">&#9654;</span>
          </span>
        </button>
      )}
      <div className="p-4">
        {meta ? (
          <>
            <p className="font-medium leading-snug">{meta.title}</p>
            <p className="text-sm text-muted-foreground">{meta.author_name}</p>
          </>
        ) : (
          <div className="space-y-2">
            <Skeleton className="h-4 w-3/4" />
            <Skeleton className="h-3 w-1/3" />
          </div>
        )}
      </div>
    </Card>
  )
}
