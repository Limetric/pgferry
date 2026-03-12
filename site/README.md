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

Recommended Pages settings:

- Root directory: `site`
- Build command: `npm run build`
- Build output directory: `dist`
- Production branch: `main`

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
