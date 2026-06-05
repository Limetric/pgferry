// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import remarkGfm from 'remark-gfm';

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

const head = [
	{
		tag: 'meta',
		attrs: { name: 'theme-color', content: '#11243c' },
	},
];

export default defineConfig({
	site,
	trailingSlash: 'always',
	// Starlight 0.39 wires its own remark plugins into the MDX pipeline, which
	// stops GFM from being applied to .mdx files (tables render as raw text).
	// Apply remark-gfm explicitly so both .md and .mdx get GFM tables.
	markdown: {
		remarkPlugins: [remarkGfm],
	},
	integrations: [
		starlight({
			title: 'pgferry',
			logo: {
				src: '/public/favicon.svg',
			},
			description: 'Reliable MySQL, SQLite, and MSSQL migrations into PostgreSQL.',
			social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/Limetric/pgferry' }],
			editLink: {
				baseUrl: 'https://github.com/Limetric/pgferry/edit/main/site/src/content/docs/',
			},
			components: {
				PageFrame: './src/components/PageFrame.astro',
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
					label: 'Start Here',
					items: [{ autogenerate: { directory: 'get-started' } }],
				},
				{
					label: 'Migration Patterns',
					items: [{ autogenerate: { directory: 'migration-patterns' } }],
				},
				{
					label: 'Guides',
					items: [{ autogenerate: { directory: 'guides' } }],
				},
				{
					label: 'Examples',
					items: [{ autogenerate: { directory: 'examples' } }],
				},
				{
					label: 'Operations',
					items: [{ autogenerate: { directory: 'operations' } }],
				},
				{
					label: 'Reference',
					items: [{ autogenerate: { directory: 'reference' } }],
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
