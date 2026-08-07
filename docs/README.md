# Website

This website is built using [Docusaurus 2](https://docusaurus.io/), a modern static website generator.

### Installation

```
$ npm install
```

### Local Development

```
$ npm run start
```

This command starts a local development server and opens up a browser window. Most changes are reflected live without having to restart the server.

### Build

```
$ npm run build
```

This command generates static content into the `build` directory and can be served using any static contents hosting service.

### Deployment

Deployment is automated. Any push to `main` that touches `docs/**` triggers the
[Deploy Docs](../.github/workflows/docs-deploy.yml) workflow, which builds the site and
publishes `build/` to Cloudflare Workers Static Assets.

The site is served at [docs.husonym.com](https://docs.husonym.com), configured as a
custom domain in [`wrangler.jsonc`](./wrangler.jsonc). Wrangler manages the DNS record
itself, so there is no `CNAME` file to maintain.

The workflow needs two repository secrets: `CLOUDFLARE_API_TOKEN` (with the *Edit
Cloudflare Workers* template) and `CLOUDFLARE_ACCOUNT_ID`.

To deploy by hand:

```
$ npm run build
$ npx wrangler deploy
```
