import type { Config } from "tailwindcss";

const config: Config = {
  darkMode: ["class"],
  content: ["./src/**/*.{js,ts,jsx,tsx}"],
  theme: {
    extend: {
      colors: {
        base: {
          white: "#FFFFFF",
          50: "#FDFCF9",
          100: "#F8F6F3",
          200: "#ECE4DD",
          300: "#DCD2C6",
          400: "#AF9F8A",
          500: "#827057",
          600: "#5C543C",
          700: "#4A402C",
          800: "#31231B",
          900: "#20190F",
          950: "#231810",
        },
        gold: {
          50: "#FFFDFA",
          100: "#FFF7E9",
          200: "#FCE7C2",
          300: "#F5CA78",
          400: "#EBB03C",
          500: "#E19614",
          600: "#BE7E10",
          700: "#9A6410",
          800: "#7A4F10",
          900: "#5C3C0C",
          950: "#3D2808",
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
        background: "hsl(var(--background) / <alpha-value>)",
        surface: "hsl(var(--surface) / <alpha-value>)",
        "surface-raised": "hsl(var(--surface-raised) / <alpha-value>)",
        foreground: "hsl(var(--foreground) / <alpha-value>)",
        border: "hsl(var(--border) / <alpha-value>)",
      },
      textColor: {
        primary: "rgba(var(--alpha-dark), 1)",
        secondary: "rgba(var(--alpha-dark), 0.8)",
        muted: "rgba(var(--alpha-dark), 0.6)",
        faint: "rgba(var(--alpha-dark), 0.4)",
      },
      borderColor: {
        alpha: "rgba(var(--alpha-dark), 0.1)",
        "alpha-strong": "rgba(var(--alpha-dark), 0.2)",
      },
      backgroundColor: {
        "alpha-10": "rgba(var(--alpha-dark), 0.1)",
        "alpha-5": "rgba(var(--alpha-dark), 0.05)",
      },
      fontFamily: {
        sans: ["var(--font-body)", "system-ui", "sans-serif"],
        heading: ["var(--font-display)", "Georgia", "serif"],
        display: ["var(--font-display)", "Georgia", "serif"],
        mono: ["var(--font-mono)", "monospace"],
      },
      boxShadow: {
        dropdown: "0 4px 12px rgba(35, 24, 16, 0.1)",
        card: "0 1px 3px rgba(35, 24, 16, 0.04)",
      },
      borderRadius: {
        DEFAULT: "8px",
        lg: "12px",
        md: "8px",
        sm: "6px",
      },
      keyframes: {
        "fade-in-up": {
          "0%": { opacity: "0", transform: "translateY(12px)" },
          "100%": { opacity: "1", transform: "translateY(0)" },
        },
      },
      animation: {
        "fade-in-up": "fade-in-up 0.5s cubic-bezier(0.19, 1, 0.22, 1) both",
      },
    },
  },
  plugins: [],
};

export default config;
