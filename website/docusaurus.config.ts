import { themes as prismThemes } from 'prism-react-renderer';
import type { Config } from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

// This runs in Node.js - Don't use client-side code here (browser APIs, JSX...)

const config: Config = {
  title: 'Weavster',
  tagline: 'Config-driven integration pipelines you can validate, test, and run locally.',
  favicon: 'img/favicon.ico',

  future: {
    v4: true,
  },

  // Production URL for GitHub Pages.
  url: 'https://weavster-dev.github.io',
  baseUrl: '/weavster/',

  organizationName: 'weavster-dev',
  projectName: 'weavster',

  onBrokenLinks: 'throw',

  markdown: {
    hooks: {
      onBrokenMarkdownLinks: 'warn',
    },
  },

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  presets: [
    [
      'classic',
      {
        docs: {
          sidebarPath: './sidebars.ts',
          editUrl: 'https://github.com/weavster-dev/weavster/tree/main/website/',
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    image: 'img/docusaurus-social-card.jpg',
    colorMode: {
      respectPrefersColorScheme: true,
    },
    navbar: {
      title: 'Weavster',
      logo: {
        alt: 'Weavster Logo',
        src: 'img/logo.svg',
      },
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'docsSidebar',
          position: 'left',
          label: 'Docs',
        },
        {
          href: 'https://github.com/weavster-dev/weavster',
          label: 'GitHub',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Docs',
          items: [
            { label: 'Getting Started', to: '/docs/getting-started' },
            { label: 'CLI Reference', to: '/docs/cli' },
            { label: 'Config Reference', to: '/docs/config' },
          ],
        },
        {
          title: 'Project',
          items: [
            { label: 'GitHub', href: 'https://github.com/weavster-dev/weavster' },
            { label: 'Contributing', to: '/docs/contributing' },
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} Weavster. Built with Docusaurus.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
