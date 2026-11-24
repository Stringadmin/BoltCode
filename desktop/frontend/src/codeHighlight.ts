import DOMPurify from 'dompurify'
import hljs from 'highlight.js/lib/core'
import bash from 'highlight.js/lib/languages/bash'
import css from 'highlight.js/lib/languages/css'
import go from 'highlight.js/lib/languages/go'
import javascript from 'highlight.js/lib/languages/javascript'
import json from 'highlight.js/lib/languages/json'
import python from 'highlight.js/lib/languages/python'
import rust from 'highlight.js/lib/languages/rust'
import typescript from 'highlight.js/lib/languages/typescript'
import xml from 'highlight.js/lib/languages/xml'
import yaml from 'highlight.js/lib/languages/yaml'

for (const [name, syntax] of Object.entries({
  bash, css, go, javascript, json, python, rust, typescript, xml, yaml,
})) {
  hljs.registerLanguage(name, syntax)
}

export function highlightCode(text: string, language: string): string {
  const name = language.toLowerCase()
  if (text.length > 16_000 || !hljs.getLanguage(name)) return ''

  const html = hljs.highlight(text, { language: name, ignoreIllegals: true }).value
  return DOMPurify.sanitize(html, { ALLOWED_TAGS: ['span'], ALLOWED_ATTR: ['class'] })
}
