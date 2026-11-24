<script setup lang="ts">
import { computed } from 'vue'
import DOMPurify from 'dompurify'
import { marked, type Token, type Tokens } from 'marked'
import CopyButton from './CopyButton.vue'
import { highlightCode } from './codeHighlight'

const props = defineProps<{ text: string }>()
type Block = { kind: 'markdown'; html: string } | { kind: 'code'; language: string; text: string; highlighted: string }

const blocks = computed(() => {
  const output: Block[] = []
  let prose: Token[] = []
  function appendProse() {
    if (!prose.length) return
    const html = marked.parser(prose)
    if (html.trim()) output.push({ kind: 'markdown', html: DOMPurify.sanitize(html, {
      ALLOWED_TAGS: ['p', 'br', 'strong', 'em', 'del', 'blockquote', 'pre', 'code', 'ul', 'ol', 'li',
        'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'hr', 'table', 'thead', 'tbody', 'tr', 'th', 'td'],
      ALLOWED_ATTR: [],
    }) })
    prose = []
  }
  for (const token of marked.lexer(props.text)) {
    if (token.type !== 'code') {
      prose.push(token)
      continue
    }
    appendProse()
    const code = token as Tokens.Code
    const language = code.lang?.trim().split(/\s+/)[0] || ''
    output.push({ kind: 'code', language, text: code.text, highlighted: highlightCode(code.text, language) })
  }
  appendProse()
  return output
})
</script>

<template>
  <div class="formatted-message">
    <template v-for="(part, index) in blocks" :key="index">
      <figure v-if="part.kind === 'code'" class="code-block">
        <figcaption>
          <span>{{ part.language || '代码' }}</span>
          <CopyButton :text="part.text" label="复制代码" />
        </figcaption>
        <pre><code v-if="part.highlighted" v-html="part.highlighted"></code><code v-else>{{ part.text }}</code></pre>
      </figure>
      <div v-else class="markdown" v-html="part.html"></div>
    </template>
  </div>
</template>
