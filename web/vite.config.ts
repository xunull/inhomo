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
  build: {
    // inhomo 是本机 localhost 的嵌入式面板（go:embed 打进二进制、走回环秒开），
    // 无网络/CDN，拆分主包几乎没有实际收益，故调高阈值压掉这个纯提示性的分块大小警告。
    // 首屏体积若真成问题，再按路由 React.lazy 拆分（拓扑页已如此），而非在此。
    chunkSizeWarningLimit: 2000,
  },
})
