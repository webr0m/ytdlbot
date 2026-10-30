import { defineConfig } from 'vite';
export default defineConfig({build:{outDir:'../internal/app/static',emptyOutDir:true},server:{proxy:{'/api':'http://127.0.0.1:8095','/healthz':'http://127.0.0.1:8095'}}});
