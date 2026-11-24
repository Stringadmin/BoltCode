export interface Appearance {
  theme: 'system' | 'light' | 'dark'
  accent: 'indigo' | 'teal' | 'berry'
  background: 'default' | 'paper' | 'slate'
  chatSize: number
  codeSize: number
}

export const defaultAppearance: Appearance = {
  theme: 'system', accent: 'indigo', background: 'default', chatSize: 15, codeSize: 12,
}

export function normalizeAppearance(value: unknown): Appearance {
  const candidate = (value && typeof value === 'object' ? value : {}) as Partial<Appearance>
  return {
    theme: candidate.theme === 'dark' || candidate.theme === 'light' ? candidate.theme : 'system',
    accent: candidate.accent === 'teal' || candidate.accent === 'berry' ? candidate.accent : 'indigo',
    background: candidate.background === 'paper' || candidate.background === 'slate' ? candidate.background : 'default',
    chatSize: fontSize(candidate.chatSize, 13, 20, defaultAppearance.chatSize),
    codeSize: fontSize(candidate.codeSize, 11, 17, defaultAppearance.codeSize),
  }
}

function fontSize(value: unknown, min: number, max: number, fallback: number) {
  return typeof value === 'number' && Number.isFinite(value) ? Math.max(min, Math.min(max, Math.round(value))) : fallback
}

export function applyAppearance(value: Appearance) {
  const root = document.documentElement
  root.dataset.theme = value.theme
  root.dataset.accent = value.accent
  root.dataset.background = value.background
  root.style.setProperty('--body-size', value.chatSize + 'px')
  root.style.setProperty('--body-line', Math.round(value.chatSize * 26 / 15) + 'px')
  root.style.setProperty('--code-size', value.codeSize + 'px')
}
