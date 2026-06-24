/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        // ============================================
        // Theme tokens resolve to CSS custom properties so every generated
        // utility (bg-primary, text-text-muted, border-border, …) re-keys per
        // [data-theme]. Raw values live in src/index.css: @theme is the dark
        // ("Obsidian Observatory") default; :root[data-theme='light'] is
        // "Observatory Day". Do not hardcode hexes here — it would defeat the
        // light-mode override.
        // ============================================
        background: 'var(--color-background)',
        surface: {
          DEFAULT: 'var(--color-surface)',
          elevated: 'var(--color-surface-elevated)',
          highlight: 'var(--color-surface-highlight)',
        },
        border: {
          DEFAULT: 'var(--color-border)',
          subtle: 'var(--color-border-subtle)',
        },

        // Primary - Warm Amber (Energy, Activity)
        primary: {
          DEFAULT: 'var(--color-primary)',
          light: 'var(--color-primary-light)',
          dark: 'var(--color-primary-dark)',
        },

        // Secondary - Deep Teal (Technical, Data)
        secondary: {
          DEFAULT: 'var(--color-secondary)',
          light: 'var(--color-secondary-light)',
          dark: 'var(--color-secondary-dark)',
        },

        // Tertiary - Purple/Violet (Agents, AI)
        tertiary: {
          DEFAULT: 'var(--color-tertiary)',
          light: 'var(--color-tertiary-light)',
          dark: 'var(--color-tertiary-dark)',
        },

        // Status colors
        status: {
          running: 'var(--color-status-running)',
          stopped: 'var(--color-status-stopped)',
          error: 'var(--color-status-error)',
          pending: 'var(--color-status-pending)',
        },

        // Text hierarchy
        text: {
          primary: 'var(--color-text-primary)',
          secondary: 'var(--color-text-secondary)',
          muted: 'var(--color-text-muted)',
        },
      },
      fontFamily: {
        sans: ['Outfit', 'system-ui', 'sans-serif'],
        mono: ['IBM Plex Mono', 'Fira Code', 'monospace'],
      },
      // Shadows resolve to theme-scoped vars (src/index.css) so glow becomes a
      // soft drop-shadow on light and depth shadows lighten appropriately.
      boxShadow: {
        'sm': 'var(--shadow-sm)',
        'md': 'var(--shadow-md)',
        'lg': 'var(--shadow-lg)',
        'node': 'var(--shadow-node)',
        'node-hover': 'var(--shadow-node-hover)',
        'pane-left': 'var(--shadow-pane-left)',
        'glow-primary': 'var(--shadow-glow-primary)',
        'glow-secondary': 'var(--shadow-glow-secondary)',
        'glow-tertiary': 'var(--shadow-glow-tertiary)',
        'glow-success': '0 0 12px var(--color-status-running-glow)',
        'glow-error': '0 0 12px var(--color-status-error-glow)',
      },
      animation: {
        'fade-in-up': 'fade-in-up 0.4s cubic-bezier(0.4, 0, 0.2, 1) forwards',
        'fade-in-scale': 'fade-in-scale 0.3s cubic-bezier(0.4, 0, 0.2, 1) forwards',
        'slide-in-right': 'slide-in-right 0.3s cubic-bezier(0.4, 0, 0.2, 1) forwards',
        'pulse-glow': 'pulse-glow 2s ease-in-out infinite',
        'status-pulse': 'status-pulse 2s ease-in-out infinite',
      },
      keyframes: {
        'fade-in-up': {
          '0%': { opacity: '0', transform: 'translateY(12px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' },
        },
        'fade-in-scale': {
          '0%': { opacity: '0', transform: 'scale(0.95)' },
          '100%': { opacity: '1', transform: 'scale(1)' },
        },
        'slide-in-right': {
          '0%': { opacity: '0', transform: 'translateX(20px)' },
          '100%': { opacity: '1', transform: 'translateX(0)' },
        },
        'pulse-glow': {
          '0%, 100%': { boxShadow: '0 0 12px rgba(245, 158, 11, 0.1)' },
          '50%': { boxShadow: '0 0 24px rgba(245, 158, 11, 0.2)' },
        },
        'status-pulse': {
          '0%, 100%': { transform: 'scale(1)', opacity: '1' },
          '50%': { transform: 'scale(1.5)', opacity: '0' },
        },
      },
      backdropBlur: {
        'xs': '4px',
      },
      borderRadius: {
        'xl': '12px',
        '2xl': '16px',
      },
    },
  },
  plugins: [],
}
