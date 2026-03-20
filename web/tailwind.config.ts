import type { Config } from "tailwindcss";

const config: Config = {
  content: ["./src/**/*.{js,ts,jsx,tsx}"],
  theme: {
    extend: {
      colors: {
        timber: {
          50: "#FDF8F3",
          100: "#F5E6D3",
          200: "#E8CCAA",
          300: "#D4A574",
          400: "#B8834A",
          500: "#9A6B35",
          600: "#7C5428",
          700: "#5F3F1E",
          800: "#432C15",
          900: "#2A1A0C",
        },
        harvest: {
          50: "#FFFBEB",
          100: "#FEF3C7",
          200: "#FDE68A",
          300: "#FCD34D",
          400: "#FBBF24",
          500: "#F59E0B",
          600: "#D97706",
          700: "#B45309",
        },
        grove: {
          50: "#F0F9F0",
          100: "#DCEFDC",
          200: "#B8DFB8",
          500: "#4A7C4A",
          600: "#3D6B3D",
          700: "#2F5A2F",
        },
        ember: {
          50: "#FEF2F2",
          500: "#C75050",
          600: "#B83B3B",
          700: "#9B2C2C",
        },
        stone: {
          50: "#FAF8F5",
          100: "#F3EFE8",
          200: "#E5DED4",
          300: "#CFC5B6",
          400: "#A89B8A",
          500: "#857766",
          600: "#6B5E4F",
          700: "#524639",
          800: "#3A3028",
          900: "#231C15",
        },
        background: "hsl(var(--background) / <alpha-value>)",
        surface: "hsl(var(--surface) / <alpha-value>)",
        "surface-raised": "hsl(var(--surface-raised) / <alpha-value>)",
        foreground: "hsl(var(--foreground) / <alpha-value>)",
        border: "hsl(var(--border) / <alpha-value>)",
      },
      fontFamily: {
        sans: ["var(--font-body)", "system-ui", "sans-serif"],
        heading: ["var(--font-heading)", "Georgia", "serif"],
        mono: ["var(--font-mono)", "monospace"],
      },
      boxShadow: {
        warm: "0 2px 8px rgba(58, 48, 40, 0.08)",
      },
      borderRadius: {
        lg: "var(--radius)",
        md: "calc(var(--radius) - 2px)",
      },
    },
  },
  plugins: [],
};

export default config;
