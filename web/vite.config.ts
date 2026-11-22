import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  // The app is served under a gateway prefix that may or may not be stripped,
  // so all asset URLs must stay relative.
  base: './',
  build: {
    outDir: '../internal/webdist/dist',
    // The directory also holds the committed dist/.keep that keeps a fresh
    // checkout compilable, so it must not be emptied. Asset names are hashed.
    emptyOutDir: false,
  },
})
