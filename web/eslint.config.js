import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'
import { defineConfig, globalIgnores } from 'eslint/config'

// Raw Tailwind palette classes do not re-key per theme, so anything written this
// way is styled for whichever theme the author happened to be looking at. That
// is how the light theme ended up rendering amber text at 1.2:1. Semantic tokens
// (text-status-pending, bg-primary, …) resolve to var(--color-*) and follow
// [data-theme]; see web/src/components/README.md.
//
// Scoped to amber and yellow because those are the families now fully migrated.
// The emerald, violet, rose, and sky literals are the same latent bug and should
// be added here as each one is migrated, so this rule only ever ratchets.
const RAW_PALETTE =
  String.raw`/\b(?:text|bg|border|ring|fill|stroke|from|via|to|divide|outline|decoration|accent|caret|shadow|placeholder)-(?:amber|yellow)-\d{2,3}\b/`

const RAW_PALETTE_MESSAGE =
  'Raw Tailwind palette class. These do not re-key per theme and break the light theme. ' +
  'Use a semantic token instead (text-status-pending, bg-primary, border-status-error, …).'

export default defineConfig([
  globalIgnores(['dist', 'coverage']),
  {
    files: ['**/*.{ts,tsx}'],
    // Chart series colors are a categorical palette rather than a status
    // vocabulary and need their own contrast pass; tests assert on class strings.
    ignores: ['src/components/chart/**', 'src/**/*.test.{ts,tsx}', 'src/__tests__/**'],
    rules: {
      'no-restricted-syntax': [
        'error',
        { selector: `Literal[value=${RAW_PALETTE}]`, message: RAW_PALETTE_MESSAGE },
        { selector: `TemplateElement[value.raw=${RAW_PALETTE}]`, message: RAW_PALETTE_MESSAGE },
      ],
    },
  },
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
      reactHooks.configs.flat.recommended,
      reactRefresh.configs.vite,
    ],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
    },
    rules: {
      // Underscore prefix marks intentionally unused bindings (noop handler
      // args, ignored destructured props).
      '@typescript-eslint/no-unused-vars': [
        'error',
        {
          argsIgnorePattern: '^_',
          varsIgnorePattern: '^_',
          caughtErrorsIgnorePattern: '^_',
          destructuredArrayIgnorePattern: '^_',
        },
      ],
    },
  },
])
