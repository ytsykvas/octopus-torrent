import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

// Own sources must stay at exactly 100% for every metric, per file (QA-02).
// Generated Wails bindings live outside src/ (frontend/wailsjs) and are not own code.
export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    setupFiles: ['src/test/setup.ts'],
    restoreMocks: true,
    coverage: {
      provider: 'v8',
      include: ['src/**/*.{ts,tsx}'],
      exclude: ['src/**/*.test.{ts,tsx}', 'src/test/**'],
      reporter: ['text', 'json-summary'],
      thresholds: { lines: 100, statements: 100, functions: 100, branches: 100, perFile: true },
    },
  },
})
