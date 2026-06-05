import { defineConfig } from 'tsup';

// Bundle the CLI for publishing. @weavster/core is a devDependency, so tsup
// inlines it into the output; the runtime third-party deps stay external and
// are installed from npm via package.json "dependencies".
export default defineConfig({
  entry: ['src/index.ts'],
  format: ['esm'],
  outDir: 'dist',
  target: 'node20',
  clean: true,
  external: ['ajv', 'commander', 'yaml', 'jiti', 'fast-xml-parser'],
});
