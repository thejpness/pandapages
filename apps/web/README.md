# Panda Pages web

The frontend uses Vue, TypeScript, Vite, Tailwind CSS, and a generated service
worker. Use the exact Node release in `.nvmrc` and npm release in
`packageManager`; the Docker build and CI use the same versions.

```sh
nvm use
npm install --global npm@12.0.1
npm ci
```

npm 12 blocks dependency install scripts unless they are explicitly approved
in `package.json`. Do not use blanket script approval. Review a newly reported
script and pin only the required package/version with `npm install-scripts`.

## Checks

```sh
npm test
npm run lint
npm run typecheck
npm run build
```

The production application uses same-origin API requests. Only variables with
the `VITE_` prefix are loaded by the frontend build. The service worker
precaches versioned static assets and the application shell; it deliberately
does not cache authenticated API responses.
