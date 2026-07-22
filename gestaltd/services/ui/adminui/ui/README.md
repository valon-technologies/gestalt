# gestaltd admin UI

React SPA for `/admin`, built with Vite and vendored components from the Valon shadcn registry (`toolshed/valon-tools/registry`).

## Build

```bash
cd gestaltd/services/ui/adminui/ui
bun install
bunx @tanstack/router-cli generate
bun run build
```

The static bundle is emitted to `../out/` and embedded by `adminui.go`.

When updating registry components, copy the canonical sources from `toolshed/valon-tools/apps/registry/ui/src/ui/` into `src/components/ui/` (or re-run `shadcn add` against the built registry artifacts from a toolshed checkout).
