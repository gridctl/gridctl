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
        // OBSIDIAN OBSERVATORY - Color System
        // ============================================

        // Core Obsidian Palette
        background: '#08080a',
        surface: {
          DEFAULT: '#111113',
          elevated: '#18181b',
          highlight: '#1f1f23',
        },
        border: {
          DEFAULT: '#27272a',
          subtle: 'rgba(255, 255, 255, 0.06)',
        },

        // Primary - Warm Amber (Energy, Activity)
        primary: {
          DEFAULT: '#f59e0b',
          light: '#fbbf24',
          dark: '#d97706',
        },

        // Secondary - Deep Teal (Technical, Data)
        secondary: {
          DEFAULT: '#0d9488',
          light: '#14b8a6',
          dark: '#0f766e',
        },

        // Tertiary - Purple/Violet (Agents, AI)
        tertiary: {
          DEFAULT: '#8b5cf6',
          light: '#a78bfa',
          dark: '#7c3aed',
        },

        // Status colors
        status: {
          running: '#10b981',
          stopped: '#52525b',
          error: '#f43f5e',
          pending: '#eab308',
        },

        // Text hierarchy - Warm whites
        text: {
          primary: '#fafaf9',
          secondary: '#a8a29e',
          muted: '#78716c',
        },
      },
      fontFamily: {
        sans: ['Outfit', 'system-ui', 'sans-serif'],
        mono: ['IBM Plex Mono', 'Fira Code', 'monospace'],
      },
      boxShadow: {
        'sm': '0 1px 2px rgba(0, 0, 0, 0.5)',
        'md': '0 4px 12px rgba(0, 0, 0, 0.4), 0 2px 4px rgba(0, 0, 0, 0.3)',
        'lg': '0 8px 32px rgba(0, 0, 0, 0.5), 0 4px 12px rgba(0, 0, 0, 0.3)',
        'node': '0 4px 24px rgba(0, 0, 0, 0.4)',
        'node-hover': '0 8px 40px rgba(0, 0, 0, 0.5), 0 0 0 1px rgba(255, 255, 255, 0.05)',
        'glow-primary': '0 0 20px rgba(245, 158, 11, 0.15), 0 0 40px rgba(245, 158, 11, 0.1)',
        'glow-secondary': '0 0 20px rgba(13, 148, 136, 0.15), 0 0 40px rgba(13, 148, 136, 0.1)',
        'glow-tertiary': '0 0 20px rgba(139, 92, 246, 0.15), 0 0 40px rgba(139, 92, 246, 0.1)',
        'glow-success': '0 0 12px rgba(16, 185, 129, 0.2)',
        'glow-error': '0 0 12px rgba(244, 63, 94, 0.2)',
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
