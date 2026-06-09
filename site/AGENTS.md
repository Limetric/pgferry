# AGENTS.md

Astro Starlight documentation site for pgferry.

## Package management

Use Bun for package management in this directory.

```bash
bun install                  # Install dependencies
bun run dev                  # Start the dev server
bun run build                # Build the site
bun run check-routes         # Verify generated routes after a build
```

## Dev server

The dev server runs behind portless. To get the URL:

```bash
portless get pgferry
```
