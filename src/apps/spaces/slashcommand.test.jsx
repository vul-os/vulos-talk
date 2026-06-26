/**
 * slashcommand.test.jsx — /slash-command autocomplete.
 * Covers the parse/complete helpers and the picker popup (appears + completes).
 */
import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import SlashCommandPicker, { parseSlashQuery, completeSlash } from './SlashCommandPicker.jsx'

describe('parseSlashQuery', () => {
  it('detects a leading slash command being typed', () => {
    expect(parseSlashQuery('/dep', 4)).toEqual({ query: 'dep' })
  })
  it('returns null when text does not start with /', () => {
    expect(parseSlashQuery('hello /dep', 10)).toBeNull()
  })
  it('returns null once the cursor moves past the command token', () => {
    expect(parseSlashQuery('/deploy now', 9)).toBeNull()
  })
  it('matches the empty query right after typing /', () => {
    expect(parseSlashQuery('/', 1)).toEqual({ query: '' })
  })
})

describe('completeSlash', () => {
  it('replaces the leading token with the chosen command name', () => {
    expect(completeSlash('/dep', 'deploy')).toBe('/deploy ')
  })
  it('preserves trailing text after the first space', () => {
    expect(completeSlash('/dep staging', 'deploy')).toBe('/deploy staging')
  })
})

describe('SlashCommandPicker', () => {
  const commands = [
    { name: 'deploy', description: 'Trigger a deploy' },
    { name: 'giphy', description: 'Post a gif' },
  ]

  it('renders matching commands (popup appears)', () => {
    render(<SlashCommandPicker commands={commands} query="dep" onSelect={vi.fn()} onClose={vi.fn()} />)
    expect(screen.getByText('/deploy')).toBeTruthy()
    expect(screen.queryByText('/giphy')).toBeNull()
  })

  it('renders nothing when no command matches', () => {
    const { container } = render(<SlashCommandPicker commands={commands} query="zzz" onSelect={vi.fn()} onClose={vi.fn()} />)
    expect(container.firstChild).toBeNull()
  })

  it('completes a command on click', () => {
    const onSelect = vi.fn()
    render(<SlashCommandPicker commands={commands} query="gip" onSelect={onSelect} onClose={vi.fn()} />)
    fireEvent.click(screen.getByText('/giphy'))
    expect(onSelect).toHaveBeenCalledWith('giphy')
  })

  it('completes the highlighted command on Enter key', () => {
    const onSelect = vi.fn()
    render(<SlashCommandPicker commands={commands} query="" onSelect={onSelect} onClose={vi.fn()} />)
    fireEvent.keyDown(window, { key: 'Enter' })
    expect(onSelect).toHaveBeenCalledWith('deploy')
  })
})
