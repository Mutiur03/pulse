# @pulse/vite-plugin

Gives every chunk a debug id and uploads the build's source maps to Pulse, so
browser errors show original files, lines and code.

```ts
// vite.config.ts
import { defineConfig } from 'vite';
import { pulseSourcemaps } from '@pulse/vite-plugin';

export default defineConfig({
  build: { sourcemap: 'hidden' },
  plugins: [
    pulseSourcemaps({
      release: process.env.RELEASE!, // same value as the SDK's `release`
      project: 1,
      token: process.env.PULSE_UPLOAD_TOKEN, // Settings > Upload tokens
      url: 'https://pulse.example.com/api',
      deleteAfterUpload: true,
    }),
  ],
});
```

Without a token the plugin only injects debug ids, so the maps can be uploaded
later with `pulse-cli sourcemaps upload`.
