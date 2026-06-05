import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vitest/config';

// Resolve @weavster/core to its TypeScript source so cli tests run without
// building core first. Runtime (tsx dev, built dist) still uses the package's
// dist entry via normal resolution.
export default defineConfig({
  resolve: {
    alias: {
      '@weavster/core': fileURLToPath(new URL('../core/src/index.ts', import.meta.url)),
    },
  },
});
