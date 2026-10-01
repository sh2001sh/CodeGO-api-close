import { defineConfig } from '@rsbuild/core'
import { pluginReact } from '@rsbuild/plugin-react'
import tailwind from '@tailwindcss/postcss'

export default defineConfig({
  plugins: [pluginReact()],
  source: { entry: { index: './src/main.tsx' } },
  html: { template: './index.html' },
  server: {
    host: '127.0.0.1',
    port: 3100,
    proxy: {
      '/api': {
        target: process.env.V3_CONTROL_URL ?? 'http://127.0.0.1:3002',
        changeOrigin: false,
      },
    },
  },
  output: {
    distPath: { root: 'dist' },
    filename: { js: '[name].[contenthash:8].js', css: '[name].[contenthash:8].css' },
  },
  tools: { postcss: { postcssOptions: { plugins: [tailwind()] } } },
})
