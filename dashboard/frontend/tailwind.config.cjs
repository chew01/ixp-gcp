/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        surface: "#0d1117",
        panel: "#161b22",
        border: "#30363d",
        accent: "#00e5ff",
        "accent-dim": "#0097a7",
        success: "#3fb950",
        warning: "#d29922",
        danger: "#f85149",
      },
      fontFamily: {
        mono: ["'JetBrains Mono'", "monospace"],
        sans: ["'Inter'", "sans-serif"],
      },
    },
  },
  plugins: [],
};
