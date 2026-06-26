import { writeFileSync } from 'node:fs'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// emptyOutDir wipes dist/ on every build, including the dist/.gitkeep
// placeholder that lets `go build` (//go:embed all:dist) compile before any
// frontend build exists. Recreate it after the bundle is written.
const keepGitkeep = {
  name: 'keep-dist-gitkeep',
  closeBundle() {
    writeFileSync('dist/.gitkeep', '')
  },
}

// Vulos Talk: monolithic SPA build (dist/) embedded into the Go binary.
export default defineConfig({
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test-setup.js'],
    include: ['src/**/*.test.{js,jsx}'],
  },
  plugins: [react(), keepGitkeep],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    chunkSizeWarningLimit: 2000,
    rollupOptions: {
      output: {
        manualChunks: {
          'vendor-react': ['react', 'react-dom', 'react-router-dom'],
        },
      },
    },
  },
  server: {
    port: 5175,
    proxy: {
      '/api': { target: 'http://localhost:8080', changeOrigin: true },
    },
  },
})
