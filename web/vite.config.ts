import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// 开发期把 /api 代理到运行中的 inhomo serve 后端（HMR + 真实数据）。
// 发布时 `npm run build` 出 dist/，由 Go 侧 go:embed 打进二进制。
// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': 'http://127.0.0.1:8464',
    },
  },
})
