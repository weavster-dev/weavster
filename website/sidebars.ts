import type { SidebarsConfig } from '@docusaurus/plugin-content-docs';

// Explicit sidebar matching the MVP documentation IA.
const sidebars: SidebarsConfig = {
  docsSidebar: [
    'getting-started',
    'concepts',
    'cli',
    'config',
    'formats',
    'dsl',
    'typescript',
    'testing',
    'architecture',
    'contributing',
  ],
};

export default sidebars;
