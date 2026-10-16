import type { Config } from "tailwindcss";
import animate from "tailwindcss-animate";

// Design tokens: docs/12-ui-design.md §3–5. Colors resolve to CSS variables in globals.css.
const config: Config = {
  darkMode: ["class"],
  content: [
    "./app/**/*.{ts,tsx}",
    "./components/**/*.{ts,tsx}",
    "./hooks/**/*.{ts,tsx}",
    "./providers/**/*.{ts,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        border: "hsl(var(--border))",
        input: "hsl(var(--border))",
        ring: "hsl(var(--ring))",
        background: "hsl(var(--background))",
        foreground: "hsl(var(--foreground))",
        primary: { DEFAULT: "hsl(var(--primary))", foreground: "hsl(var(--primary-foreground))" },
        secondary: { DEFAULT: "hsl(var(--secondary))", foreground: "hsl(var(--foreground))" },
        muted: { DEFAULT: "hsl(var(--muted))", foreground: "hsl(var(--muted-foreground))" },
        accent: { DEFAULT: "hsl(var(--accent))", foreground: "hsl(var(--foreground))" },
        success: "hsl(var(--success))",
        warning: "hsl(var(--warning))",
        destructive: {
          DEFAULT: "hsl(var(--destructive))",
          foreground: "hsl(var(--primary-foreground))",
        },
        card: { DEFAULT: "hsl(var(--card))", foreground: "hsl(var(--card-foreground))" },
        // Stitch surface ramp (login + shell chrome).
        surface: {
          DEFAULT: "#fbf8ff",
          low: "#f4f2fd",
          container: "#eeedf7",
          high: "#e8e7f1",
          highest: "#e3e1ec",
          lowest: "#ffffff",
        },
        outline: { DEFAULT: "#737686", variant: "#c3c6d7" },
        // Scanner viewfinder backdrop (Stitch barscanner_ISBN).
        "inverse-surface": "#2f3038",
        "on-surface": { DEFAULT: "#1a1b22", variant: "#434655" },
        status: {
          "available-bg": "#DCFCE7",
          "available-fg": "#15803D",
          "available-dot": "#16A34A",
          "out-bg": "#FFEDD5",
          "out-fg": "#C2410C",
          "out-dot": "#EA580C",
          "na-bg": "#FEE2E2",
          "na-fg": "#B91C1C",
          "na-dot": "#DC2626",
        },
      },
      borderRadius: { lg: "0.5rem", md: "0.375rem", sm: "0.25rem" },
      // Scanner "red shake" on an unusable barcode (docs/11 §8).
      keyframes: {
        shake: {
          "0%, 100%": { transform: "translateX(0)" },
          "20%, 60%": { transform: "translateX(-6px)" },
          "40%, 80%": { transform: "translateX(6px)" },
        },
      },
      animation: { shake: "shake 0.4s ease-in-out" },
      fontFamily: {
        sans: [
          "var(--font-inter)",
          "var(--font-noto-sc)",
          "system-ui",
          "-apple-system",
          "sans-serif",
        ],
        mono: ["var(--font-jetbrains)", "ui-monospace", "SFMono-Regular", "Menlo", "monospace"],
      },
    },
  },
  plugins: [animate],
};

export default config;
