// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

function normalizeURL(value) {
	if (!value) return null;
	const trimmed = value.trim();
	if (!trimmed) return null;
	const candidate =
		trimmed.startsWith('http://') || trimmed.startsWith('https://') ? trimmed : `https://${trimmed}`;
	try {
		const url = new URL(candidate);
		if (url.protocol !== 'http:' && url.protocol !== 'https:') return null;
		return url.toString().replace(/\/$/, '');
	} catch {
		return null;
	}
}

const site =
	normalizeURL(process.env.SITE_URL) ||
	normalizeURL(process.env.VERCEL_PROJECT_PRODUCTION_URL) ||
	normalizeURL(process.env.VERCEL_URL) ||
	'https://pgferry.com';
const plausibleDomain = process.env.PUBLIC_PLAUSIBLE_DOMAIN || '';
const plausibleSrc = process.env.PUBLIC_PLAUSIBLE_SRC || '';
const plausibleAPI = process.env.PUBLIC_PLAUSIBLE_API || '';

const head = [
	{
		tag: 'meta',
		attrs: { name: 'theme-color', content: '#11243c' },
	},
];

if (plausibleDomain && plausibleSrc) {
	head.push({
		tag: 'script',
		attrs: {
			defer: true,
			src: plausibleSrc,
			'data-domain': plausibleDomain,
			...(plausibleAPI ? { 'data-api': plausibleAPI } : {}),
		},
	});
}

export default defineConfig({
	site,
	trailingSlash: 'always',
	integrations: [
		starlight({
			title: 'pgferry',
			description: 'Reliable MySQL, SQLite, and MSSQL migrations into PostgreSQL.',
			social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/Limetric/pgferry' }],
			editLink: {
				baseUrl: 'https://github.com/Limetric/pgferry/edit/main/site/src/content/docs/',
			},
			lastUpdated: true,
			customCss: ['/src/styles/custom.css'],
			favicon: '/favicon.svg',
			head,
			sidebar: [
				{
					label: 'Overview',
					link: '/',
				},
				{
					label: 'Get Started',
					autogenerate: { directory: 'get-started' },
				},
				{
					label: 'Reference',
					autogenerate: { directory: 'reference' },
				},
				{
					label: 'Examples',
					autogenerate: { directory: 'examples' },
				},
				{
					label: 'Project',
					items: [
						{
							label: 'GitHub Releases',
							link: 'https://github.com/Limetric/pgferry/releases',
							attrs: { target: '_blank', rel: 'noreferrer' },
						},
						{
							label: 'Issue Tracker',
							link: 'https://github.com/Limetric/pgferry/issues',
							attrs: { target: '_blank', rel: 'noreferrer' },
						},
					],
				},
			],
		}),
	],
});
