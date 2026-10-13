// @ts-nocheck -- The Node test runner loads TSX through Vite's SSR transformer.
import test, { before, after } from 'node:test'
import assert from 'node:assert/strict'
import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { createServer } from 'vite'

let server, Markdown, markdownURL
before(async () => {
  server = await createServer({ server: { middlewareMode: true, watch: null }, appType: 'custom' })
  const module = await server.ssrLoadModule('/src/Markdown.tsx')
  Markdown = module.Markdown
  markdownURL = module.markdownURL
})
after(async () => { await server?.close() })

const render = (body) => renderToStaticMarkup(React.createElement(Markdown, { children: body }))

test('provider comments render headings, emphasis, lists, quotes and readable fenced code', () => {
  const html = render('# Review\n\n**Important** and *careful*.\n\n- First\n- Second\n\n1. Start\n2. Finish\n\n> A quoted reply\n\n```ts\nconst tag = "<script>";\n```')
  assert.match(html, /<h1>Review<\/h1>/)
  assert.match(html, /<strong>Important<\/strong>/)
  assert.match(html, /<em>careful<\/em>/)
  assert.match(html, /<ul>[\s\S]*<li>First<\/li>/)
  assert.match(html, /<ol>[\s\S]*<li>Start<\/li>/)
  assert.match(html, /<blockquote>/)
  assert.match(html, /<pre[^>]*tabindex="0"[^>]*><code class="language-ts">/)
  assert.match(html, /&lt;script&gt;/)
})

test('GitHub tables, task lists, strikethrough and emoji shortcodes render without changing code', () => {
  const html = render('| Result | Status |\n| --- | --- |\n| Build | :white_check_mark: |\n\n- [x] Reviewed **code**\n- [ ] Follow up\n\n~~Old~~ :+1: :rocket: 😄\n\n`:rocket:`\n\n```\n:+1:\n```')
  assert.match(html, /<table>/)
  assert.match(html, /<th>Result<\/th>/)
  assert.match(html, /✅/)
  assert.match(html, /type="checkbox" disabled="" checked=""/)
  assert.match(html, /type="checkbox" disabled=""/)
  assert.match(html, /<del>Old<\/del>/)
  assert.match(html, /👍 🚀 😄/)
  assert.match(html, /<code>:rocket:<\/code>/)
  assert.match(html, /<code>:\+1:\n<\/code>/)
})

test('raw HTML and executable or credential-bearing links cannot become active DOM content', () => {
  const html = render('<script>window.compromised = true</script>\n\n<img src="x" onerror="alert(1)">\n\n[script](javascript:alert%281%29) [file](file:///etc/passwd) [data](data:text/html,test) [secret](https://user:password@example.com/)\n\n![unsafe](data:image/svg+xml,test)')
  assert.doesNotMatch(html, /<script|<img|onerror=|href="(?:javascript:|file:|data:|https:\/\/user:)/)
  assert.match(html, /markdown-invalid-link/)
  assert.match(html, /unsafe/)
})

test('web links retain accessible labels and local footnotes retain their anchors', () => {
  const html = render('[Docs](https://github.com/wailsapp/wails)\n\nA note[^1].\n\n[^1]: Footnote.')
  assert.match(html, /href="https:\/\/github.com\/wailsapp\/wails"[^>]*target="_blank"[^>]*rel="noopener noreferrer"/)
  assert.match(html, /opens in browser/)
  assert.match(html, /id="user-content-fnref-1"/)
  assert.match(html, /href="#user-content-fn-1"/)
})

test('URL validation rejects malformed, obfuscated, relative and non-web protocols', () => {
  for (const value of ['javascript:alert(1)', 'java\nscript:alert(1)', 'file:///tmp/a', 'data:text/html,<script>', '//example.com', '/relative', 'mailto:x@example.com', 'https://a:b@example.com', 'https://exa mple.com', 'https://example.com/\u0000']) {
    assert.equal(markdownURL(value), undefined, value)
  }
  assert.equal(markdownURL('https://example.com/docs?q=1#section'), 'https://example.com/docs?q=1#section')
  assert.equal(markdownURL('http://example.com/'), 'http://example.com/')
  assert.equal(markdownURL('#user-content-fn-1'), '#user-content-fn-1')
})
