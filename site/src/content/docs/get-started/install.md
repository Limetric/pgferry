---
title: Install
description: Install pgferry from releases or build it from source.
sidebar:
  order: 1
---

`pgferry` is a single Go binary. There are no sidecar services, no runtime agents, and no extra database middleware to deploy.

## Download a release

Pick the latest binary for your platform directly:

<div class="download-grid">
  <article class="download-card">
    <h3>macOS</h3>
    <p>Choose the binary that matches your Mac: Apple Silicon for M-series Macs, Intel for older hardware.</p>
    <div class="download-actions">
      <a class="download-button" href="https://github.com/Limetric/pgferry/releases/latest/download/pgferry-darwin-arm64">Apple Silicon / ARM64</a>
      <a class="download-button" href="https://github.com/Limetric/pgferry/releases/latest/download/pgferry-darwin-amd64">Intel / AMD64</a>
    </div>
  </article>
  <article class="download-card">
    <h3>Linux</h3>
    <p>AMD64 fits most Intel and AMD hosts. ARM64 is for Graviton, Ampere, and similar systems.</p>
    <div class="download-actions">
      <a class="download-button" href="https://github.com/Limetric/pgferry/releases/latest/download/pgferry-linux-amd64">AMD64</a>
      <a class="download-button" href="https://github.com/Limetric/pgferry/releases/latest/download/pgferry-linux-arm64">ARM64</a>
    </div>
  </article>
  <article class="download-card">
    <h3>Windows</h3>
    <p>AMD64 fits standard PCs. ARM64 is for Windows on ARM devices. Both ship as <code>.exe</code> binaries.</p>
    <div class="download-actions">
      <a class="download-button" href="https://github.com/Limetric/pgferry/releases/latest/download/pgferry-windows-amd64.exe">AMD64</a>
      <a class="download-button" href="https://github.com/Limetric/pgferry/releases/latest/download/pgferry-windows-arm64.exe">ARM64</a>
    </div>
  </article>
</div>

For the changelog and the full release page, use [GitHub Releases](https://github.com/Limetric/pgferry/releases/latest).

On Linux and macOS, make the binary executable after downloading:

```bash
chmod +x pgferry
```

You can either move it somewhere on your PATH:

```bash
sudo mv pgferry /usr/local/bin/
pgferry version
```

Or run it directly from wherever you downloaded it:

```bash
./pgferry version
```

The rest of the docs use `pgferry` without `./` — just adjust if you haven't added it to your PATH.

## Build from source

```bash
git clone https://github.com/Limetric/pgferry.git
cd pgferry
go build -o build/pgferry .
./build/pgferry version
```

## What the binary expects

- A source DSN for MySQL, MariaDB, SQLite, or MSSQL.
- A target PostgreSQL DSN.
- A TOML config file describing schema, type mapping, and migration behavior.

## Next step

Move to [Quick Start](/get-started/quick-start/) to create a minimal config and run your first migration.
