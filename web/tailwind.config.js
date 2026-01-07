/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        // Core palette - Quantum Neon
        background: '#0D0D0D',       // Rich Black
        surface: '#1a1a1a',          // Liquid Glass base
        surfaceHighlight: '#2a2a2a', // Highlight
        border: '#3a3a3a',           // Border

        // Neon Accents
        primary: '#00CAFF',          // Electric Cyan - MCP servers
        primaryLight: '#66DFFF',     // Light Cyan
        secondary: '#B915CC',        // Futuristic Purple - Resources
        secondaryLight: '#D45DE8',   // Light Purple

        // Status colors
        status: {
          running: '#2CFF05',        // Neon Green
          stopped: '#4a4a4a',        // Dark Gray
          error: '#FF3366',          // Neon Red
          pending: '#FFCC00',        // Neon Yellow
        },

        // Text hierarchy
        text: {
          primary: '#f8fafc',        // White
          secondary: '#a0a0a0',      // Light Gray
          muted: '#707070',          // Muted Gray
        },
      },
      fontFamily: {
        sans: ['Inter', 'system-ui', 'sans-serif'],
        mono: ['JetBrains Mono', 'Consolas', 'monospace'],
      },
      boxShadow: {
        'node': '0 4px 20px rgba(0, 0, 0, 0.6)',
        'node-hover': '0 8px 30px rgba(0, 0, 0, 0.7)',
        'glow-primary': '0 0 20px rgba(0, 202, 255, 0.4)',
        'glow-secondary': '0 0 20px rgba(185, 21, 204, 0.4)',
      },
      animation: {
        'pulse-slow': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite',
        'glow': 'glow 2s ease-in-out infinite alternate',
      },
      keyframes: {
        glow: {
          '0%': { boxShadow: '0 0 5px rgba(59, 130, 246, 0.2)' },
          '100%': { boxShadow: '0 0 20px rgba(59, 130, 246, 0.4)' },
        },
      },
    },
  },
  plugins: [],
}
