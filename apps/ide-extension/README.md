# EnvNexus Sync (VS Code extension)

Sync MCP configuration and related assets from [EnvNexus](https://github.com/zayeagle/envnexus) into your workspace (for example under `.cursor/`).

## Requirements

- Visual Studio Code **1.80.0** or newer (see `engines.vscode` in `package.json`).

## Development

From this directory:

```bash
npm install
npm run compile
```

Use `npm run watch` for watch mode during development.

## Package a `.vsix`

The [`@vscode/vsce`](https://www.npmjs.com/package/@vscode/vsce) CLI is included as a dev dependency. The `vscode:prepublish` script runs a production webpack build before packaging.

```bash
npm install
npm run package
```

This produces a file named `envnexus-sync-<version>.vsix` in the current directory (version comes from `package.json`).

## Install the packed extension

1. In VS Code: **Extensions** view → `…` (Views and More Actions) → **Install from VSIX…** and select the generated `.vsix` file, **or**
2. From a terminal: `code --install-extension envnexus-sync-<version>.vsix`

## Commands

| Command            | Description        |
| ------------------ | ------------------ |
| **EnvNexus: Login** | Device flow login; token is stored in the extension’s secret storage. |
| **EnvNexus: Sync**  | Fetches the IDE sync manifest and writes files under `.cursor/` (e.g. MCP config, skills). |

## Repository

Monorepo: <https://github.com/zayeagle/envnexus>
