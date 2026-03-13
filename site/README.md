# pgferry Site

Astro + Starlight site for the `pgferry` docs and marketing surface.

## Local development

Run from `site/`:

```bash
bun install --frozen-lockfile
bun run dev
```

Build locally:

```bash
bun run build
bun run check-routes
bun run preview
```

## Vercel

This site can be deployed on Vercel using the built-in Git integration. Astro static sites deploy on Vercel with zero framework-specific configuration once the project is imported and the correct root directory is selected. Sources: [Astro on Vercel](https://vercel.com/docs/frameworks/frontend/astro), [Project settings](https://vercel.com/docs/project-configuration/project-settings).

### Recommended project settings

When importing the repository into Vercel:

- Framework Preset: `Astro`
- Root Directory: `site`
- Install Command: `bun install --frozen-lockfile`
- Build Command: `bun run build`
- Output Directory: `dist`
- Production Branch: `main`

This repo also includes [`vercel.json`](./vercel.json) so the Bun install/build commands are versioned instead of living only in the dashboard.

### Environment variables

Add these in Vercel under Project Settings -> Environment Variables:

- `SITE_URL=https://pgferry.com`
- `PUBLIC_PLAUSIBLE_DOMAIN=pgferry.com`
- `PUBLIC_PLAUSIBLE_SRC=https://your-plausible-host/js/script.js`
- `PUBLIC_PLAUSIBLE_API=https://your-plausible-host/api/event` (optional)

`SITE_URL` should be a full absolute URL with protocol. Example: `https://pgferry.com`, not just `pgferry.com`.

Vercel applies environment variables per environment and changes only affect new deployments, so redeploy after editing them. Sources: [Environment variables](https://vercel.com/docs/environment-variables), [Managing environment variables](https://vercel.com/docs/environment-variables/managing-environment-variables).

### Custom domain

After the first successful deployment:

1. Open the Vercel project.
2. Go to Settings -> Domains.
3. Add `pgferry.com`.
4. If you also add `www.pgferry.com`, configure the domain redirect in the Vercel Domains dashboard so one canonical host wins consistently.
5. Update your DNS records to the values Vercel shows for the project.

Vercel documents apex domains with an `A` record and subdomains with a `CNAME`, but you should use the exact records shown in your project because they may be account-specific. Sources: [Setting up a custom domain](https://vercel.com/docs/domains/set-up-custom-domain), [Adding & configuring a custom domain](https://vercel.com/docs/domains/working-with-domains/add-a-domain).

### Deploy flow

- Push to `main` for production deployments.
- Push other branches or open pull requests for preview deployments.

That behavior is built into Vercel's Git-based deployment model. Source: [Deploying to Vercel](https://vercel.com/docs/deployments).

## Build hygiene

The repository CI runs:

```bash
bun install --frozen-lockfile
bun run build
bun run check-routes
```

`check-routes` verifies expected docs routes, internal links, canonical tags, sitemap output, and `robots.txt`.

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
