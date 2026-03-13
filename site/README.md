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

## Vercel

This site can be deployed on Vercel using the built-in Git integration. Astro static sites deploy on Vercel with zero framework-specific configuration once the project is imported and the correct root directory is selected. Sources: [Astro on Vercel](https://vercel.com/docs/frameworks/frontend/astro), [Project settings](https://vercel.com/docs/project-configuration/project-settings).

### Recommended project settings

When importing the repository into Vercel:

- Framework Preset: `Astro`
- Root Directory: `site`
- Install Command: `npm clean-install --progress=false`
- Build Command: `npm run build`
- Output Directory: `dist`
- Production Branch: `main`

If Vercel auto-detects Astro after you set `site` as the root directory, you can keep the detected defaults.

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
4. Optionally add `www.pgferry.com` and redirect it to the apex domain.
5. Update your DNS records to the values Vercel shows for the project.

Vercel documents apex domains with an `A` record and subdomains with a `CNAME`, but you should use the exact records shown in your project because they may be account-specific. Sources: [Setting up a custom domain](https://vercel.com/docs/domains/set-up-custom-domain), [Adding & configuring a custom domain](https://vercel.com/docs/domains/working-with-domains/add-a-domain).

### Deploy flow

- Push to `main` for production deployments.
- Push other branches or open pull requests for preview deployments.

That behavior is built into Vercel's Git-based deployment model. Source: [Deploying to Vercel](https://vercel.com/docs/deployments).

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
