import { defineConfig } from 'vite';

// The build in dist/ is embedded into spikehub (cmd/spikehub/web), which serves
// index.html at its root and the bundle under assets/. Capacitor and Tauri load
// the same files from their own origins, so every URL in the build is relative.
export default defineConfig({
  base: './',
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
});
