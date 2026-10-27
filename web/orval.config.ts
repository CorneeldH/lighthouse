import { defineConfig } from 'orval'

// `npm run gen` regenerates src/api/ from openapi.json, which the Go server's
// TestOpenAPISnapshot writes. Plain fetch with relative paths: no query layer, and no
// `formatter`, so the output does not depend on a prettier install.
export default defineConfig({
  lighthouse: {
    input: './openapi.json',
    output: {
      target: './src/api/lighthouse.ts',
      client: 'fetch',
      mode: 'single',
    },
  },
})
