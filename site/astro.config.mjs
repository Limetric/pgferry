// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

const site = process.env.SITE_URL || 'https://pgferry.com';
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
					items: [
						{ slug: 'get-started/install' },
						{ slug: 'get-started/quick-start' },
						{ slug: 'get-started/plan-and-validate' },
					],
				},
				{
					label: 'Reference',
					items: [
						{ slug: 'reference/configuration' },
						{ slug: 'reference/type-mapping' },
						{ slug: 'reference/migration-pipeline' },
						{ slug: 'reference/conventions' },
						{ slug: 'reference/hooks' },
					],
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
