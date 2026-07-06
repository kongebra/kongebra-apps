// Admin-appen serveres på ett delt vertsnavn under en subpath (SPEC §10):
//   Host(`leikan.newb.no`) && PathPrefix(`/admin`) -> admin-appen
// Traefik videresender /admin/* UENDRET (ingen strip), så både ruter, assets og
// server-ruter (auth, healthz) ligger under /admin. Denne konstanten er den ene
// sannhetskilden i app-koden. Den MÅ matche `base` i vite.config.ts (Vite kan
// ikke importere denne fila trygt fra sin egen config, så verdien gjentas der
// med en kryss-referanse - samme mønster som TanStack sitt custom-basepath-eksempel).
export const BASE_PATH = '/admin'

// Prefikser en app-intern sti med basepath. Brukes til rå <a>-navigasjon og
// redirects mot server-ruter (/auth/*, /healthz) som ligger utenfor rute-treet
// og derfor ikke får basepath automatisk fra TanStack Router.
export function withBase(path: string): string {
  return `${BASE_PATH}${path.startsWith('/') ? path : `/${path}`}`
}
