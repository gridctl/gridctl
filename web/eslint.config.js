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
// The ratchet is complete: every default Tailwind color family is rejected.
const RAW_PALETTE =
  String.raw`/\b(?:text|bg|border|ring|fill|stroke|from|via|to|divide|outline|decoration|accent|caret|shadow|placeholder)-(?:amber|yellow|emerald|green|violet|purple|red|rose|pink|sky|blue|cyan|teal|gray|slate|zinc|neutral|stone|orange|lime|indigo|fuchsia)-\d{2,3}\b/`

const RAW_PALETTE_MESSAGE =
  'Raw Tailwind palette class. These do not re-key per theme and break the light theme. ' +
  'Use a semantic token instead (text-status-pending, bg-primary, border-status-error, …).'

// A slash modifier on a text token blends the text into its surface and drops
// it below WCAG AA. The only exemption is a decorative icon that carries
// aria-hidden on the same element.
const TEXT_OPACITY =
  String.raw`/(?<![\w-])(?:[\w\[\]&>*.-]+:)*text-(?:text-(?:muted|secondary|primary)|primary)\/\d+/`

const ARIA_HIDDEN_CLASSNAME =
  'JSXOpeningElement:has(> JSXAttribute[name.name="aria-hidden"]' +
  ':not([value.value="false"]):not([value.expression.value=false]))' +
  ' > JSXAttribute[name.name="className"] *'

const TEXT_OPACITY_MESSAGE =
  'Opacity modifier on a text token. Use text-text-muted, text-text-secondary, or text-primary at full opacity. ' +
  'The modifier is allowed only on a decorative icon with aria-hidden on the same element.'

export default defineConfig([
  globalIgnores(['dist', 'coverage']),
  {
    files: ['**/*.{ts,tsx}'],
    // Tests assert on class strings and are not shipped styles.
    ignores: ['src/**/*.test.{ts,tsx}', 'src/__tests__/**'],
    rules: {
      'no-restricted-syntax': [
        'error',
        { selector: `Literal[value=${RAW_PALETTE}]`, message: RAW_PALETTE_MESSAGE },
        { selector: `TemplateElement[value.raw=${RAW_PALETTE}]`, message: RAW_PALETTE_MESSAGE },
        { selector: `Literal[value=${TEXT_OPACITY}]:not(${ARIA_HIDDEN_CLASSNAME})`, message: TEXT_OPACITY_MESSAGE },
        { selector: `TemplateElement[value.raw=${TEXT_OPACITY}]:not(${ARIA_HIDDEN_CLASSNAME})`, message: TEXT_OPACITY_MESSAGE },
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
