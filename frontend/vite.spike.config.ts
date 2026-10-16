import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The engine spike's page (task 007; task 034 deletes the spike). It is built into the spike app's
// own embed directory, so the product's dist never contains it. The directory is not named dist:
// wails build would copy frontend/dist over it.
export default defineConfig({
  root: 'spike',
  plugins: [react()],
  build: {
    outDir: '../../cmd/engine-spike/assets',
    emptyOutDir: true,
  },
})
