import { SITE } from "../consts.ts";

interface PostSeo {
  title: string;
  description: string;
  url: string;
  pubDate: Date;
  updatedDate?: Date;
  image: string;
}

/** schema.org BlogPosting - the rich-result payload for a post. */
export function blogPostingJsonLd(p: PostSeo) {
  return {
    "@context": "https://schema.org",
    "@type": "BlogPosting",
    headline: p.title,
    description: p.description,
    image: p.image,
    datePublished: p.pubDate.toISOString(),
    dateModified: (p.updatedDate ?? p.pubDate).toISOString(),
    author: { "@type": "Person", name: SITE.author },
    publisher: { "@type": "Organization", name: SITE.title },
    mainEntityOfPage: { "@type": "WebPage", "@id": p.url },
    inLanguage: SITE.lang,
  };
}

/** schema.org BreadcrumbList from [label, url] pairs. */
export function breadcrumbJsonLd(items: { name: string; url: string }[]) {
  return {
    "@context": "https://schema.org",
    "@type": "BreadcrumbList",
    itemListElement: items.map((item, i) => ({
      "@type": "ListItem",
      position: i + 1,
      name: item.name,
      item: item.url,
    })),
  };
}
