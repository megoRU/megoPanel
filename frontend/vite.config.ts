import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vite';

const viteConfig = defineConfig({
  plugins: [
    react(),
    tailwindcss(),
  ],
  server: {
    host: '0.0.0.0',
    port: 8889,
    proxy: {
      '/api': 'http://localhost:8888',
    },
  },
});

export default viteConfig;
