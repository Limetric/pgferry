# pgferry Site

Astro + Starlight site for the `pgferry` docs and marketing surface.

## Local development

Run from `site/`:

```bash
npm install
npm run dev
```

Build locally:

```bash
npm run build
npm run preview
```

## Cloudflare Pages

If you use the minimal GitHub Actions deployment flow, keep Cloudflare pointed at the Pages project but do not rely on dashboard builds.

## GitHub Actions deployment

The repository includes [site-deploy.yml](/home/atlas/Documents/Projects/pgferry/.github/workflows/site-deploy.yml), which:

- installs dependencies from `site/package-lock.json`
- builds the Astro site from `site/`
- runs `wrangler pages deploy site/dist --project-name=pgferry`

Required GitHub repository secrets:

- `CLOUDFLARE_API_TOKEN`
- `CLOUDFLARE_ACCOUNT_ID`
- `PUBLIC_PLAUSIBLE_SRC`
- `PUBLIC_PLAUSIBLE_API` (optional)

The workflow currently assumes:

- Cloudflare Pages project name: `pgferry`
- production site URL: `https://pgferry.com`
- Plausible domain: `pgferry.com`

If those change, update the workflow env values.

## Cloudflare dashboard

For the Pages project itself:

- connect the custom domain to the `pgferry` Pages project
- disable automatic production builds if the project is Git-integrated
- do not use `npx wrangler deploy` as a dashboard deploy command for this setup

## Plausible

The site only injects the Plausible script when the relevant environment variables are present:

- `SITE_URL`
- `PUBLIC_PLAUSIBLE_DOMAIN`
- `PUBLIC_PLAUSIBLE_SRC`
- `PUBLIC_PLAUSIBLE_API` (optional)

Example values for a self-hosted instance:

```bash
SITE_URL=https://pgferry.com
PUBLIC_PLAUSIBLE_DOMAIN=pgferry.com
PUBLIC_PLAUSIBLE_SRC=https://plausible.example.com/js/script.js
PUBLIC_PLAUSIBLE_API=https://plausible.example.com/api/event
```
