import { defineConfig } from 'vite'
import { tanstackStart } from '@tanstack/react-start/plugin/vite'
import { nitroV2Plugin } from '@tanstack/nitro-v2-vite-plugin'
import viteReact from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// TanStack Start (SSR) + Tailwind v4 (Vite-plugin, ingen config-fil).
// nitroV2Plugin (preset node-server) gir en selv-lyttende Node-SSR-server i
// .output/server/index.mjs - deployes i en slank distroless-node-runtime.
// Path-alias @/* løses nativt av Vite via tsconfig (resolve.tsconfigPaths).
export default defineConfig({
  server: {
    // Aspire injiserer PORT lokalt; fall tilbake til 3000.
    port: Number(process.env.PORT) || 3000,
  },
  resolve: {
    tsconfigPaths: true,
  },
  plugins: [
    tailwindcss(),
    tanstackStart(),
    nitroV2Plugin({ preset: 'node-server' }),
    viteReact(),
  ],
})
