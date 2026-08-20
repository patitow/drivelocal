import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
      '@/components': path.resolve(__dirname, './src/components'),
      '@/lib': path.resolve(__dirname, './src/lib'),
      '@/hooks': path.resolve(__dirname, './src/hooks'),
      '@/types': path.resolve(__dirname, './src/types'),
      '@/utils': path.resolve(__dirname, './src/utils'),
    },
  },
  server: {
    host: '0.0.0.0',
    port: parseInt(process.env.PORT || '3000'), // Porta padrão para dev (sem privilégios admin)
    strictPort: false, // Se 3000 estiver ocupada, usa outra e avisa
    hmr: {
      host: 'localhost',
    },
    watch: {
      usePolling: false,
      interval: 100,
    },
    proxy: {
      '/api': {
        target: process.env.API_PROXY_TARGET || process.env.VITE_API_URL || 'http://localhost:5000',
        changeOrigin: true,
        secure: false,
        ws: true, // Enable websocket proxying for HMR
        configure: (proxy, _options) => {
          proxy.on('error', (err, _req, _res) => {
            console.log('Proxy error:', err);
          });
        },
      }
    }
  },
  build: {
    outDir: '../dist',
    emptyOutDir: true,
    rollupOptions: {
      output: {
        manualChunks: {
          vendor: ['react', 'react-dom'],
          router: ['@tanstack/react-router'],
          query: ['@tanstack/react-query'],
          ui: ['@radix-ui/react-dialog', '@radix-ui/react-dropdown-menu'],
        },
      },
    },
  },
  optimizeDeps: {
    include: ['react', 'react-dom', '@tanstack/react-router', '@tanstack/react-query'],
  },
})

