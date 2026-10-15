# Language-server catalog

Settings → Language intelligence contains **57 servers**, with local logos, searchable language/file names, category filters, Installed and Updates views, install progress/cancellation, and inline executable configuration.

**40 servers have managed install/update recipes.** The other 17 use an existing SDK or upstream installer and expose Configure and Setup instructions. Availability means that the executable and required files have been detected; a server still needs a compatible project and runtime to return useful results. Hover and definitions depend on the server; this catalog does not add editor completion, diagnostic squiggles, or automatic project dependency installation.

## Installation

| Method | Prerequisites and behavior |
| --- | --- |
| npm (28) | Node.js 22.22.2+ with npm. Exact top-level versions, private prefix/cache/config, lifecycle scripts disabled. TypeScript/Vue/Astro/MDX include the pinned TypeScript SDK. |
| PHP (1) | PHP 8.2+ with mbstring, tokenizer, and posix. Phpactor PHAR is checked against its published SHA-256. Native Windows PHP lacks posix. |
| Go (7) | Go 1.24+; an upstream release can require newer Go. Exact module version, official module proxy/checksum database, private caches, no automatic toolchain downloads. Helm also needs GCC or Clang. |
| Python (4) | Python 3.10+ with venv/ensurepip. Private virtual environment, exact top-level package, PyPI wheels only. Current CMake/Nginx packages require Python below 3.14. Robot Framework is included as a server dependency. |
| Existing SDK (17) | Install from the linked upstream instructions, then detect it from PATH or configure an executable. These installations are updated through their own package manager. |

Installation does not change repository dependency manifests or replace custom executable settings. Failed updates keep the previous installation active. Update checks fetch metadata only for installed managed tools, cache successful results for six hours, and retain pending-update state when offline. **Check for updates** bypasses the cache. See [language intelligence](LANGUAGE_INTELLIGENCE.md) for settings locations, source snapshots, and navigation boundaries.

## Servers

### Configuration

| Server | Example files | Setup |
| --- | --- | --- |
| [YAML](https://github.com/redhat-developer/yaml-language-server) | `*.yaml`, `*.yml` | Install / update · Node |
| [Dockerfile](https://github.com/rcjsuen/dockerfile-language-server-nodejs) | `Dockerfile`, `Dockerfile.*`, `*.dockerfile` | Install / update · Node |
| [Docker Compose](https://github.com/microsoft/compose-language-service) | `compose.yaml`, `compose.yml`, `compose.*.yaml` | Install / update · Node |
| [GitHub Actions](https://github.com/actions/languageservices) | `.github/workflows/*.yml`, `.github/workflows/*.yaml`, `action.yml` | Install / update · Node |
| [Ansible](https://github.com/ansible/vscode-ansible) | `*.ansible.yml`, `*.ansible.yaml`, `ansible.yml` | Install / update · Node |
| [Azure Pipelines](https://github.com/microsoft/azure-pipelines-language-server) | `azure-pipelines.yml`, `azure-pipelines.yaml`, `azure-pipelines.*.yml` | Install / update · Node |
| [Jsonnet](https://github.com/grafana/jsonnet-language-server) | `*.jsonnet`, `*.libsonnet` | Install / update · Go |
| [CUE](https://cuelang.org/docs/concept/using-the-cue-language-server/) | `*.cue` | Install / update · Go |
| [Rego](https://github.com/kitagry/regols) | `*.rego` | Install / update · Go |
| [Helm](https://github.com/mrjosh/helm-ls) | `**/templates/**/*.yaml`, `**/templates/**/*.yml`, `**/templates/**/*.tpl` | Install / update · Go |
| [Terraform / HCL](https://github.com/hashicorp/terraform-ls) | `*.tf`, `*.tfvars`, `*.hcl` | Install / update · Go |
| [CMake](https://github.com/regen100/cmake-language-server) | `CMakeLists.txt`, `*.cmake` | Install / update · Python |
| [Nginx](https://github.com/pappasam/nginx-language-server) | `nginx.conf`, `*.nginx`, `*.nginx.conf` | Install / update · Python |
| [TOML · Taplo](https://taplo.tamasfe.dev/cli/installation.html) | `*.toml` | Existing SDK / executable |

### Data

| Server | Example files | Setup |
| --- | --- | --- |
| [JSON / JSONC](https://github.com/microsoft/vscode-json-languageservice) | `*.json`, `*.jsonc`, `*.code-workspace` | Install / update · Node |
| [GraphQL](https://www.npmjs.com/package/graphql-language-service-cli) | `*.graphql`, `*.gql` | Install / update · Node |
| [Prisma](https://github.com/prisma/language-tools) | `*.prisma` | Install / update · Node |
| [SQL · sqls](https://github.com/sqls-server/sqls) | `*.sql` | Existing SDK / executable |
| [Turtle / RDF](https://github.com/stardog-union/stardog-language-servers) | `*.ttl`, `*.n3`, `*.trig` | Install / update · Node |
| [jq](https://github.com/wader/jq-lsp) | `*.jq` | Install / update · Go |

### Languages

| Server | Example files | Setup |
| --- | --- | --- |
| [Phpactor](https://phpactor.readthedocs.io/en/master/usage/language-server.html) | `*.php`, `*.phtml`, `*.inc` | Install / update · PHP |
| [Bash / Shell](https://github.com/bash-lsp/bash-language-server) | `*.sh`, `*.bash`, `*.zsh` | Install / update · Node |
| [Python · Pyright](https://github.com/microsoft/pyright) | `*.py`, `*.pyi` | Install / update · Node |
| [Cucumber / Gherkin](https://github.com/cucumber/language-server) | `*.feature` | Install / update · Node |
| [Solidity](https://github.com/NomicFoundation/hardhat-vscode) | `*.sol` | Install / update · Node |
| [Vim script](https://github.com/iamcco/vim-language-server) | `*.vim`, `.vimrc` | Install / update · Node |
| [AWK](https://github.com/Beaglefoot/awk-language-server) | `*.awk` | Install / update · Node |
| [Perl · PerlNavigator](https://github.com/bscan/PerlNavigator) | `*.pl`, `*.pm`, `*.t` | Install / update · Node |
| [Go · gopls](https://go.dev/gopls/) | `*.go` | Install / update · Go |
| [Fortran · fortls](https://fortls.fortran-lang.org/) | `*.f`, `*.f90`, `*.f95` | Install / update · Python |
| [Robot Framework](https://github.com/robocorp/robotframework-lsp) | `*.robot`, `*.resource` | Install / update · Python |
| [Rust · rust-analyzer](https://rust-analyzer.github.io/book/installation.html) | `*.rs` | Existing SDK / executable |
| [C / C++ · clangd](https://clangd.llvm.org/installation) | `*.c`, `*.h`, `*.cc` | Existing SDK / executable |
| [Java · Eclipse JDT LS](https://github.com/eclipse-jdtls/eclipse.jdt.ls) | `*.java` | Existing SDK / executable |
| [C# · csharp-ls](https://github.com/razzmatazz/csharp-language-server) | `*.cs` | Existing SDK / executable |
| [Kotlin](https://github.com/Kotlin/kotlin-lsp) | `*.kt`, `*.kts` | Existing SDK / executable |
| [SystemVerilog / Verilog](https://github.com/imc-trading/svlangserver) | `*.sv`, `*.svh`, `*.v` | Install / update · Node |
| [Swift · SourceKit-LSP](https://github.com/swiftlang/sourcekit-lsp) | `*.swift` | Existing SDK / executable |
| [Lua · LuaLS](https://luals.github.io/wiki/getting-started/) | `*.lua` | Existing SDK / executable |
| [Zig · ZLS](https://zigtools.org/zls/install/) | `*.zig`, `*.zon` | Existing SDK / executable |
| [Ruby · Ruby LSP](https://shopify.github.io/ruby-lsp/) | `*.rb`, `*.rake`, `Gemfile` | Existing SDK / executable |
| [Elixir · ElixirLS](https://elixir-lsp.github.io/elixir-ls/) | `*.ex`, `*.exs` | Existing SDK / executable |
| [OCaml · OCaml-LSP](https://github.com/ocaml/ocaml-lsp) | `*.ml`, `*.mli` | Existing SDK / executable |
| [Dart / Flutter](https://dart.dev/tools/dart-language-server) | `*.dart` | Existing SDK / executable |

### Markup

| Server | Example files | Setup |
| --- | --- | --- |
| [MDX](https://github.com/mdx-js/mdx-analyzer/tree/main/packages/language-server) | `*.mdx` | Install / update · Node |
| [Markdown · Marksman](https://github.com/artempyanykh/marksman) | `*.md`, `*.markdown` | Existing SDK / executable |
| [LaTeX · TexLab](https://github.com/latex-lsp/texlab) | `*.tex`, `*.bib` | Existing SDK / executable |
| [XML · LemMinX](https://github.com/eclipse-lemminx/lemminx) | `*.xml`, `*.xsd`, `*.xsl` | Existing SDK / executable |

### Web

| Server | Example files | Setup |
| --- | --- | --- |
| [TypeScript / JavaScript](https://github.com/typescript-language-server/typescript-language-server) | `*.ts`, `*.mts`, `*.cts` | Install / update · Node |
| [Vue](https://github.com/vuejs/language-tools) | `*.vue` | Install / update · Node |
| [HTML](https://github.com/microsoft/vscode-html-languageservice) | `*.html`, `*.htm`, `*.xhtml` | Install / update · Node |
| [CSS / SCSS / Less](https://github.com/microsoft/vscode-css-languageservice) | `*.css`, `*.scss`, `*.sass` | Install / update · Node |
| [Astro](https://github.com/withastro/language-tools) | `*.astro` | Install / update · Node |
| [Svelte](https://github.com/sveltejs/language-tools) | `*.svelte` | Install / update · Node |
| [Twig](https://github.com/moetelo/twiggy) | `*.twig` | Install / update · Node |
| [Marko](https://github.com/marko-js/language-server) | `*.marko` | Install / update · Node |
| [CoffeeScript](https://github.com/phil294/coffeesense) | `*.coffee`, `*.litcoffee` | Install / update · Node |

CMake pins its compatible pygls dependency because the upstream package currently permits incompatible major versions. That pin participates in managed dependency-update detection.

## File routing and project setup

The native client and review editor use the same associations from [catalog.json](../internal/lspclient/catalog.json). Specific paths and names take priority: GitHub workflow files go to the Actions server; Compose names go to Compose; nested Helm templates and Ansible playbooks use their dedicated server. If the dedicated server is unavailable, an available generic YAML server can handle those YAML files. The list includes all file associations in each card’s file-type tooltip. Wildcards are case-insensitive: `*` and `?` stay within a directory, and `**/` matches zero or more directories.

Source-only configuration analysis skips copying PHP/JavaScript dependencies. PHP and web-language snapshots preserve their separate dependency handling. Configuration/initialization defaults live beside each recipe, including YAML hover/schema support. Analysis always uses the revision shown in the diff, with read-only definition grants.

Project environments still matter: language runtimes, installed dependencies, compile commands, framework configuration, and database schema connections can affect results. The client confines definition reads to its existing source/dependency grants; arbitrary SDK and system paths are not automatically granted. The catalog is not a claim that every framework, external SDK, platform, or possible server feature has been qualified. Real installer/startup smoke tests are opt-in; deterministic catalog, routing, installer, and Chromium/WebKit UI regressions run in CI.

## Adding a server

Add a definition with a unique ID, upstream setup URL, file associations/protocol language IDs, executable/stdio arguments, installation recipe, and any initialization or configuration defaults. Bundle an attributed local logo and register it in `languageServerLogos.ts`. Keep native/frontend routing tests aligned, and exercise the real installer plus LSP initialization before advertising a managed recipe.
