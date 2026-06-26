/** @type {import('tailwindcss').Config} */
// 克制深色:近黑底 + 单一靛蓝强调。去掉 Tokyo Night 的多彩,只留一个 accent。
// 设计取向:大量留白、大字排版、几何线条图标,不做赛博朋克。
export default {
  content: ['./src/**/*.{astro,html,js,jsx,md,mdx,ts,tsx}'],
  theme: {
    extend: {
      colors: {
        // 只用三个明度层,不搞 7 色调色板
        bg: '#0D0D0F',       // 页面底
        surface: '#161618',  // 卡片/终端底(和 bg 差一档)
        line: '#262629',     // 分割线/边框
        // 文字两级
        ink: '#FAFAFA',      // 主文(近白)
        dim: '#8B8B92',      // 次文(中灰)
        // 只有一个强调色——靛蓝
        accent: '#6366F1',
      },
      fontFamily: {
        // 正文用无衬线,标题/代码用等宽——两个字族够了
        sans: ['"Inter"', '-apple-system', '"Segoe UI"', 'sans-serif'],
        mono: ['"JetBrains Mono"', '"Fira Code"', 'monospace'],
      },
      letterSpacing: {
        tightish: '-0.02em',
        tighter2: '-0.04em',
      },
    },
  },
  plugins: [],
};
