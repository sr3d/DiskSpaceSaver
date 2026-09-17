import {defineConfig} from 'vite'
import react from '@vitejs/plugin-react'

// https://vitejs.dev/config/
// The dev server sits next to the Wails dev server on 34115 so the pair is
// obviously this project's, rather than fighting whatever else is on Vite's
// default 5173. strictPort makes a clash fail loudly instead of silently
// drifting to another port.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 34116,
    strictPort: true,
  },
})
