import { themes as prismThemes } from "prism-react-renderer";
import type { Config } from "@docusaurus/types";
import type * as Preset from "@docusaurus/preset-classic";

const config: Config = {
  title: "Weavster",
  tagline: "Modern Enterprise Service Bus - like dbt but for real-time transactions",
  favicon: "img/favicon.ico",

  future: {
    v4: true,
  },

  url: "https://docs.weavster.dev",
  baseUrl: "/",

  organizationName: "weavster-dev",
  projectName: "weavster",

  onBrokenLinks: "throw",

  markdown: {
    hooks: {
      onBrokenMarkdownLinks: "warn",
    }
  },
  i18n: {
    defaultLocale: "en",
    locales: ["en"],
  },

  presets: [
    [
      "classic",
      {
        docs: {
          sidebarPath: "./sidebars.ts",
          routeBasePath: "/", // Docs at root, not /docs
          editUrl: "https://github.com/weavster-dev/weavster/tree/main/docs/",
          // Versioning
          lastVersion: "current",
          versions: {
            current: {
              label: "next",
              path: "next",
              banner: "unreleased",
            },
          },
        },
        blog: false, // Disable blog
        theme: {
          customCss: "./src/css/custom.css",
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    image: "img/docusaurus-social-card.jpg",
    colorMode: {
      respectPrefersColorScheme: true,
    },
    navbar: {
      title: "Weavster",
      logo: {
        alt: "Weavster Logo",
        src: "img/logo.svg",
        href: "/next/",
      },
      items: [
        {
          type: "docSidebar",
          sidebarId: "docsSidebar",
          position: "left",
          label: "Docs",
        },
        {
          type: "docsVersionDropdown",
          position: "right",
          dropdownActiveClassDisabled: true,
        },
        {
          href: "https://weavster.dev",
          label: "Home",
          position: "right",
        },
        {
          href: "https://github.com/weavster-dev/weavster",
          label: "GitHub",
          position: "right",
        },
      ],
    },
    footer: {
      style: "dark",
      links: [
        {
          title: "Docs",
          items: [
            {
              label: "Getting Started",
              to: "/next/getting-started/installation",
            },
            {
              label: "Configuration",
              to: "/next/category/configuration",
            },
          ],
        },
        {
          title: "Community",
          items: [
            {
              label: "GitHub Discussions",
              href: "https://github.com/weavster-dev/weavster/discussions",
            },
            {
              label: "GitHub Issues",
              href: "https://github.com/weavster-dev/weavster/issues",
            },
          ],
        },
        {
          title: "More",
          items: [
            {
              label: "Main Site",
              href: "https://weavster.dev",
            },
            {
              label: "GitHub",
              href: "https://github.com/weavster-dev/weavster",
            },
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} Weavster. Built with Docusaurus.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: ["bash", "rust", "yaml", "toml"],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
