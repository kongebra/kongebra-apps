import { getCollection } from "astro:content";
import { readingTime } from "./reading-time.ts";

/** All non-draft posts (drafts allowed only in dev), newest first, with reading time. */
export async function getPosts() {
  const posts = await getCollection("posts", ({ data }) =>
    import.meta.env.PROD ? !data.draft : true,
  );
  return posts
    .map((p) => ({ ...p, minutes: readingTime(p.body ?? "") }))
    .sort((a, b) => b.data.pubDate.getTime() - a.data.pubDate.getTime());
}

export type PostWithMeta = Awaited<ReturnType<typeof getPosts>>[number];
