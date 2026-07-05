import { createFileRoute } from '@tanstack/react-router'

// GET /healthz for k8s httpGet-probe. Node SSR-serveren har ingen shell, men
// httpGet-prober trenger ingen shell (i motsetning til Docker CMD-SHELL), så
// dette holder - jf. Go-tjenestenes -health-flagg som kun finnes for skjell-løse
// Docker HEALTHCHECK-er.
export const Route = createFileRoute('/healthz')({
  server: {
    handlers: {
      GET: () =>
        new Response(JSON.stringify({ status: 'ok' }), {
          headers: { 'Content-Type': 'application/json' },
        }),
    },
  },
})
