import { useEffect, useState } from "react"
import { marked } from "marked"
import DOMPurify from "dompurify"

// Client-side render only: marked + DOMPurify need a DOM. The transcript is
// external input flowing through the LLM, so sanitize before dangerouslySet.
// ponytail: render happens post-mount, so the summary is not in the SSR HTML;
// fine for a personal tool. Move to isomorphic-dompurify if SEO ever matters.
export function Markdown({ source }: { source: string }) {
  const [html, setHtml] = useState<string | null>(null)
  useEffect(() => {
    const raw = marked.parse(source, { async: false }) as string
    setHtml(DOMPurify.sanitize(raw))
  }, [source])
  if (html === null) return <p style={{ color: "#999" }}>Rendering...</p>
  return <div dangerouslySetInnerHTML={{ __html: html }} />
}
