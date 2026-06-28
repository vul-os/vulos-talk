/**
 * sanitize.test.js — direct unit tests for the centralized DOMPurify policies
 * (src/lib/sanitize.js). These are the single source of truth for what HTML the
 * app lets through; the component-level pentests (pentest-xss, pentest-searchbar)
 * cover the render path, while this suite pins each named policy in isolation so
 * a config drift (e.g. re-allowing <iframe> or an on* handler) fails loudly.
 */
import { describe, it, expect } from 'vitest'
import {
  sanitizeChatMarkdown,
  sanitizeSearchHighlight,
  sanitizeRichHtml,
  sanitizeToText,
  stripHtml,
} from './sanitize.js'

describe('sanitizeChatMarkdown', () => {
  it('keeps safe inline formatting tags', () => {
    const html = '<strong>bold</strong> <em>i</em> <code>x</code> <a href="https://e.com">l</a>'
    const out = sanitizeChatMarkdown(html)
    expect(out).toContain('<strong>')
    expect(out).toContain('<em>')
    expect(out).toContain('<code>')
    expect(out).toContain('href="https://e.com"')
  })

  it('strips <script> entirely', () => {
    const out = sanitizeChatMarkdown('hi<script>alert(1)</script>')
    expect(out).not.toMatch(/<script/i)
    expect(out).not.toContain('alert(1)')
  })

  it('strips inline event handlers from surviving elements', () => {
    const out = sanitizeChatMarkdown('<a href="https://e.com" onclick="steal()">x</a>')
    expect(out).not.toMatch(/onclick/i)
  })

  it('drops a javascript: URL scheme on links', () => {
    const out = sanitizeChatMarkdown('<a href="javascript:alert(1)">x</a>')
    expect(out).not.toMatch(/javascript:/i)
  })

  it('removes disallowed structural tags like <img> and <iframe>', () => {
    const out = sanitizeChatMarkdown('<img src=x onerror=alert(1)><iframe src="evil"></iframe>')
    expect(out).not.toMatch(/<img/i)
    expect(out).not.toMatch(/<iframe/i)
    expect(out).not.toMatch(/onerror/i)
  })
})

describe('sanitizeSearchHighlight', () => {
  it('keeps only <mark> and discards everything else', () => {
    const out = sanitizeSearchHighlight('<mark>hit</mark><script>alert(1)</script><b>x</b>')
    expect(out).toContain('<mark>hit</mark>')
    expect(out).not.toMatch(/<script/i)
    expect(out).not.toMatch(/<b>/i)
  })
})

describe('sanitizeRichHtml', () => {
  it('forbids script/iframe/object/embed/form/input/button', () => {
    const hostile =
      '<script>x</script><iframe></iframe><object></object><embed><form></form><input><button>b</button>'
    const out = sanitizeRichHtml(hostile)
    for (const tag of ['script', 'iframe', 'object', 'embed', 'form', 'input', 'button']) {
      expect(out.toLowerCase()).not.toContain('<' + tag)
    }
  })

  it('handles null/undefined input without throwing', () => {
    expect(sanitizeRichHtml(undefined)).toBe('')
    expect(sanitizeRichHtml(null)).toBe('')
  })
})

describe('sanitizeToText / stripHtml', () => {
  it('sanitizeToText removes all markup, leaving text', () => {
    expect(sanitizeToText('<b>hello</b> <i>world</i>')).toBe('hello world')
  })

  it('sanitizeToText returns empty string for non-strings', () => {
    expect(sanitizeToText(null)).toBe('')
    expect(sanitizeToText(42)).toBe('')
  })

  it('stripHtml neutralizes a payload and returns only text content', () => {
    const out = stripHtml('<img src=x onerror=alert(1)>caption')
    expect(out).not.toMatch(/<img/i)
    expect(out).not.toMatch(/onerror/i)
    expect(out).toContain('caption')
  })
})
