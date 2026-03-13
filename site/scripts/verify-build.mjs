import { readdir, readFile, stat } from 'node:fs/promises';
import path from 'node:path';
import process from 'node:process';

const distDir = path.resolve(process.cwd(), 'dist');
const siteURL = 'https://pgferry.com';

const expectedRoutes = [
	'/',
	'/get-started/install/',
	'/get-started/quick-start/',
	'/get-started/choose-your-path/',
	'/get-started/plan-and-validate/',
	'/migration-patterns/',
	'/source-guides/',
	'/operations/',
	'/operations/first-production-migration-checklist/',
	'/operations/when-resume-is-worth-it/',
	'/operations/when-unlogged-tables-are-safe/',
	'/operations/handling-unsupported-objects-with-hooks/',
	'/reference/',
	'/reference/configuration/',
	'/reference/type-mapping/',
	'/reference/migration-pipeline/',
	'/reference/hooks/',
	'/reference/conventions-and-limitations/',
	'/examples/',
	'/examples/mysql/minimal-safe/',
	'/examples/sqlite/minimal-safe/',
	'/examples/mssql/minimal-safe/',
];

const routeMap = new Map();
const assetSet = new Set();

await crawl(distDir);

for (const route of expectedRoutes) {
	if (!routeMap.has(route)) {
		throw new Error(`missing expected route in dist: ${route}`);
	}
}

const sitemapIndex = await readText('/sitemap-index.xml');
if (!sitemapIndex.includes(`${siteURL}/sitemap-0.xml`)) {
	throw new Error('sitemap-index.xml does not reference the expected sitemap shard');
}

const sitemap = await readText('/sitemap-0.xml');
for (const route of expectedRoutes) {
	if (!sitemap.includes(`<loc>${siteURL}${route}</loc>`)) {
		throw new Error(`sitemap-0.xml is missing expected route ${route}`);
	}
}

const robots = await readText('/robots.txt');
if (!robots.includes(`Sitemap: ${siteURL}/sitemap-index.xml`)) {
	throw new Error('robots.txt is missing the expected sitemap declaration');
}

for (const [route, htmlPath] of routeMap) {
	const html = await readFile(htmlPath, 'utf8');
	if (route !== '/404.html') {
		assertIncludes(html, `<link rel="canonical" href="${siteURL}${route}"/>`, `${route} canonical`);
		assertIncludes(html, '<link rel="sitemap" href="/sitemap-index.xml"/>', `${route} sitemap link`);
	}
	for (const href of extractInternalHrefs(html)) {
		const resolved = resolveHref(route, href);
		if (resolved.kind === 'route') {
			if (!routeMap.has(resolved.value) && !assetSet.has(resolved.value)) {
				throw new Error(`broken internal link ${href} in ${route} -> ${resolved.value}`);
			}
			continue;
		}
		if (!assetSet.has(resolved.value) && !routeMap.has(resolved.value)) {
			throw new Error(`broken asset link ${href} in ${route} -> ${resolved.value}`);
		}
	}
}

console.log(`verified ${routeMap.size} routes and ${assetSet.size} assets in ${distDir}`);

async function readText(assetPath) {
	const asset = path.join(distDir, assetPath.replace(/^\//, ''));
	return readFile(asset, 'utf8');
}

async function crawl(dir) {
	for (const entry of await readdir(dir, { withFileTypes: true })) {
		const fullPath = path.join(dir, entry.name);
		if (entry.isDirectory()) {
			await crawl(fullPath);
			continue;
		}

		const relPath = path.relative(distDir, fullPath);
		if (entry.name.endsWith('.html')) {
			routeMap.set(filePathToRoute(relPath), fullPath);
			continue;
		}

		assetSet.add('/' + relPath.split(path.sep).join('/'));
	}
}

function filePathToRoute(relPath) {
	const normalized = relPath.split(path.sep).join('/');
	if (normalized === 'index.html') {
		return '/';
	}
	return `/${normalized.replace(/index\.html$/, '')}`;
}

function extractInternalHrefs(html) {
	const hrefs = new Set();
	const pattern = /href="([^"]+)"/g;
	for (const match of html.matchAll(pattern)) {
		const href = match[1];
		if (
			!href ||
			href.startsWith('#') ||
			href.startsWith('mailto:') ||
			href.startsWith('tel:') ||
			href.startsWith('javascript:') ||
			href.startsWith('http://') ||
			href.startsWith('https://')
		) {
			continue;
		}
		hrefs.add(href);
	}
	return hrefs;
}

function resolveHref(currentRoute, href) {
	const cleanHref = href.split('#', 1)[0].split('?', 1)[0];
	if (cleanHref.startsWith('/')) {
		return classifyPath(cleanHref);
	}

	const base = currentRoute.endsWith('/') ? currentRoute : `${currentRoute}/`;
	const url = new URL(cleanHref, `https://pgferry.test${base}`);
	return classifyPath(url.pathname);
}

function classifyPath(pathname) {
	if (pathname.endsWith('/')) {
		return { kind: 'route', value: pathname };
	}
	if (path.extname(pathname)) {
		return { kind: 'asset', value: pathname };
	}
	return { kind: 'route', value: `${pathname}/` };
}

function assertIncludes(haystack, needle, label) {
	if (!haystack.includes(needle)) {
		throw new Error(`missing ${label}: ${needle}`);
	}
}

await assertDistExists();

async function assertDistExists() {
	try {
		const info = await stat(distDir);
		if (!info.isDirectory()) {
			throw new Error();
		}
	} catch {
		throw new Error(`dist directory not found at ${distDir}; run npm run build first`);
	}
}
