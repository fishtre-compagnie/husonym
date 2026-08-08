// @ts-check
// Note: type annotations allow type checking and IDEs autocompletion

import type { Config } from '@docusaurus/types';
import { themes } from 'prism-react-renderer';

const config: Config = {
  title: 'Husonym',
  tagline: 'Open source Data Anonymization and Synthetic Data',
  favicon: 'img/logo_light_mode.png',
  // Set the production url of your s here
  url: 'https://docs.husonym.com',
  // Set the /<baseUrl>/ pathname under which your site is served
  // For GitHub pages deployment, it is often '/<projectName>/'
  baseUrl: '/',

  // GitHub pages deployment config.
  // If you aren't using GitHub pages, you don't need these.
  organizationName: 'fishtre-compagnie', // Usually your GitHub org/user name.
  projectName: 'husonym', // Usually your repo name.

  onBrokenLinks: 'throw',
  onBrokenMarkdownLinks: 'warn', //should probably be throw or warn but was causing a known issue in the markdown parsing of readme files from node_modules. https://github.com/facebook/docusaurus/issues/6370

  // Even if you don't use internalization, you can use this field to set useful
  // metadata like html lang. For example, if your site is Chinese, you may want
  // to replace "en" with "zh-Hans".
  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },
  plugins: [
    async function tailwindcssPlugin(context, options) {
      return {
        name: 'docusaurus-tailwindcss',
        configurePostCss(postcssOptions) {
          // Appends TailwindCSS (v4 bundles autoprefixing internally).
          postcssOptions.plugins.push(require('@tailwindcss/postcss'));
          return postcssOptions;
        },
      };
    },
  ],

  themes: [
    [
      require.resolve('@easyops-cn/docusaurus-search-local'),
      {
        hashed: true,
        indexDocs: true,
        indexBlog: false,
        // The classic docs preset is served at the site root.
        docsRouteBasePath: '/',
        language: ['en'],
      },
    ],
  ],

  presets: [
    [
      'classic',
      {
        docs: {
          routeBasePath: '/',
          sidebarPath: './sidebars.ts',
          // Remove this to remove the "edit this page" links.
          editUrl:
            'https://github.com/fishtre-compagnie/husonym/blob/main/docs',
        },
        blog: false,
        theme: {
          customCss: ['./src/css/custom.css'],
        },
      },
    ],
    [
      'docusaurus-protobuffet',
      {
        protobuffet: {
          fileDescriptorsPath: './protos/proto_docs.json',
          protoDocsPath: 'protos',
          sidebarPath: './protos/proto-sidebars.js',
        },
        docs: {
          routeBasePath: 'api',
          sidebarPath: './proto-sidebars.ts',
        },
        blog: false,
        theme: {
          customCss: require.resolve('./src/css/custom.css'),
        },
      },
    ],
  ],

  themeConfig: {
    metadata: [
      {
        name: 'keywords',
        content:
          'open source, anonymization, data anonymization, synthetic data, data privacy, data security',
      },
    ],
    image: 'img/docsOG.png',
    colorMode: {
      defaultMode: 'light',
      disableSwitch: false,
      // disabling preference until dark mode switching is fixed: https://github.com/facebook/docusaurus/issues/8938
      respectPrefersColorScheme: false,
    },
    navbar: {
      logo: {
        alt: 'Husonym',
        srcDark: 'img/logo_and_text_dark_mode.png',
        src: 'img/logo_and_text_light_mode.png',
      },

      items: [
        {
          href: 'https://github.com/fishtre-compagnie/husonym',
          position: 'right',
          className: 'header-github-link',
          'aria-label': 'GitHub repository',
        },
        { to: '/', label: 'Docs' },
        { to: '/api', label: 'SDK' },
        // The marketing site is a separate Worker (see landing/), so this has to be an
        // absolute link rather than a route.
        { href: 'https://www.husonym.com', label: 'Website', position: 'right' },
      ],
    },
    footer: {
      copyright: `Copyright © Fishtre Compagnie ${new Date().getFullYear()}`,
    },
    prism: {
      theme: themes.github,
      darkTheme: themes.dracula,
    },
  },
};

export default config;
