// Local assets work offline and do not contact a CDN from the settings page.
const logos = new Set([
  'php', 'typescript', 'vue', 'html', 'css', 'json', 'yaml', 'dockerfile', 'docker-compose', 'github-actions', 'bash', 'python', 'astro', 'svelte', 'graphql', 'prisma', 'sql', 'cucumber', 'mdx', 'solidity', 'vim', 'awk', 'perl', 'twig', 'marko', 'coffeescript', 'ansible', 'azure-pipelines', 'turtle', 'go', 'jsonnet', 'jq', 'cue', 'rego', 'helm', 'terraform', 'cmake', 'fortran', 'nginx', 'robot', 'rust', 'cpp', 'java', 'csharp', 'kotlin', 'swift', 'lua', 'markdown', 'toml', 'zig', 'latex', 'xml', 'ruby', 'elixir', 'ocaml', 'dart', 'systemverilog'
])

export function languageServerLogo(language: string): string | undefined {
  return logos.has(language) ? `/language-servers/${language}.svg` : undefined
}
