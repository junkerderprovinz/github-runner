import { writeFileSync } from 'node:fs'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  base: './',
  plugins: [
    react(),
    // The Go package embeds dist, which has to exist even before a build.
    { name: 'keep-dist', closeBundle: () => writeFileSync('dist/.gitkeep', '') },
  ],
  server: {
    proxy: { '/api': 'http://127.0.0.1:8080' },
  },
})
