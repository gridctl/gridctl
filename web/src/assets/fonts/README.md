# Vendored fonts

Latin subset woff2 files served by the web UI. Copied from fontsource 5.3.0.
They are not an npm dependency.

| File | Source | SHA-256 |
| --- | --- | --- |
| `outfit-latin-wght-normal.woff2` | `@fontsource-variable/outfit` 5.3.0, `files/outfit-latin-wght-normal.woff2` | `6c18d579fd87c3776be068b762cbc83fde3acb543d49eabd3ade842eb987e887` |
| `ibm-plex-mono-latin-400-normal.woff2` | `@fontsource/ibm-plex-mono` 5.3.0, `files/ibm-plex-mono-latin-400-normal.woff2` | `08949f728dc52d528e69b1667d15c89a5686a4ee9a296ff90983985f99c380f7` |
| `ibm-plex-mono-latin-500-normal.woff2` | `@fontsource/ibm-plex-mono` 5.3.0, `files/ibm-plex-mono-latin-500-normal.woff2` | `01d285447409c8a588692162439a038b8cbd7871309ee20267b0d2d91c6e8e22` |

Outfit and IBM Plex Mono are licensed under the SIL Open Font License 1.1.
The license text shipped in each package is:

- `web/public/fonts/LICENSE-outfit.txt` (Outfit)
- `web/public/fonts/LICENSE-ibm-plex-mono.txt` (IBM Plex Mono)

The frontend build copies those files to `dist/fonts/`. The gateway serves them
at `/fonts/LICENSE-outfit.txt` and `/fonts/LICENSE-ibm-plex-mono.txt`.
