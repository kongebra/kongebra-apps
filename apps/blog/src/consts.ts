/** Site-wide metadata. Single source of truth for SEO + feeds + OG cards. */
export const SITE = {
  url: "https://blog.kongebra.no",
  title: "kongebra",
  // Shown in <title> template, feeds, OG cards.
  tagline: "A Nordic developer hub for homelab, k3s and self-hosted platforms",
  description:
    "Field notes on running a production-grade homelab: k3s HA, GitOps, observability and the sharp edges nobody warns you about.",
  author: "Svein Are Danielsen",
  lang: "en",
  locale: "en_US",
} as const;

export const NAV = [
  { href: "/", label: "Home" },
  { href: "/posts/", label: "Writing" },
  { href: "/tags/", label: "Tags" },
] as const;
