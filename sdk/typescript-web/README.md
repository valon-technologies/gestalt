# @valon-technologies/gestalt-web

Browser REST client, session authentication, mount helpers, and Vite
integration for Gestalt web applications.

## Exports

- `@valon-technologies/gestalt-web` — browser REST client factory and auth helpers
- `@valon-technologies/gestalt-web/mount` — `base()` mount prefix helper
- `@valon-technologies/gestalt-web/vite` — Vite plugin for Gestalt dev and build

## Migration from `@valon-technologies/gestalt`

Vite and mount moved here in the alpha SDK split:

- `@valon-technologies/gestalt/vite` → `@valon-technologies/gestalt-web/vite`
- `@valon-technologies/gestalt/mount` → `@valon-technologies/gestalt-web/mount`

Provider authoring, build tooling, and server-side clients remain on
`@valon-technologies/gestalt`.
