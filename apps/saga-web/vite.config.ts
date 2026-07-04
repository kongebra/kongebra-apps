import { defineConfig } from 'vite'
import { tanstackStart } from '@tanstack/react-start/plugin/vite'
import { nitro } from 'nitro/vite'
import viteReact from '@vitejs/plugin-react'

export default defineConfig({
  server: {
    port: 3000,
  },
  plugins: [
    tanstackStart(),
    // node-server preset produces .output/server/index.mjs
    nitro({ preset: 'node-server' }),
    // React plugin must come after Start plugin
    viteReact(),
  ],
})
