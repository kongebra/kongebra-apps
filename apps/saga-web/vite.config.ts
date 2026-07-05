import { defineConfig } from 'vite'
import tailwindcss from '@tailwindcss/vite'
import { tanstackStart } from '@tanstack/react-start/plugin/vite'
import { nitro } from 'nitro/vite'
import viteReact from '@vitejs/plugin-react'

export default defineConfig({
  server: {
    port: 3000,
  },
  plugins: [
    tailwindcss(),
    tanstackStart(),
    // node-server preset produces .output/server/index.mjs
    nitro({ preset: 'node-server' }),
    // React plugin must come after Start plugin
    viteReact(),
  ],
  resolve: {
    alias: { "@": new URL("./src", import.meta.url).pathname },
  },
})
