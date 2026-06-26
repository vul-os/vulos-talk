/**
 * messagelist.test.jsx — message stream rendering: threading affordance,
 * reactions render/toggle, @mention rendering, author grouping, link preview.
 */
import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import MessageList from './MessageList.jsx'

const baseMsg = (over) => ({
  id: 'm1', channel_id: 'c1', author_id: 'alice', body: 'hello world',
  state: 'active', seq_clock: '00000000000000000001-0000000000-n', thread_parent: '',
  created_at: '2025-03-15T10:00:00Z', updated_at: '2025-03-15T10:00:00Z', ...over,
})

const noop = () => {}

function renderList(messages, props = {}) {
  return render(
    <MessageList
      messages={messages}
      onReply={noop} onEdit={noop} onDelete={noop} onPin={noop} onUnpin={noop}
      onReact={noop} onCopyLink={noop} currentUser="me" roster={[]}
      {...props}
    />
  )
}

describe('MessageList — threading', () => {
  it('shows a "N replies" affordance for a message with thread replies', () => {
    const messages = [
      baseMsg({ id: 'root', body: 'parent' }),
      baseMsg({ id: 'r1', body: 'a reply', thread_parent: 'root', author_id: 'bob', seq_clock: '00000000000000000002-0000000000-n' }),
      baseMsg({ id: 'r2', body: 'second reply', thread_parent: 'root', author_id: 'carol', seq_clock: '00000000000000000003-0000000000-n' }),
    ]
    renderList(messages)
    expect(screen.getByText(/2 replies/)).toBeTruthy()
  })

  it('opens the thread when the replies affordance is clicked', () => {
    const onReply = vi.fn()
    const messages = [
      baseMsg({ id: 'root', body: 'parent' }),
      baseMsg({ id: 'r1', thread_parent: 'root', author_id: 'bob', seq_clock: '00000000000000000002-0000000000-n' }),
    ]
    renderList(messages, { onReply })
    fireEvent.click(screen.getByText(/1 reply/))
    expect(onReply).toHaveBeenCalled()
    expect(onReply.mock.calls[0][0].id).toBe('root')
  })
})

describe('MessageList — reactions', () => {
  const messages = [baseMsg({ id: 'm1' })]
  const reactions = { m1: { '👍': { count: 2, userIds: ['alice', 'bob'] } } }

  it('renders an existing reaction with its count', () => {
    renderList(messages, { reactions })
    expect(screen.getByText('👍')).toBeTruthy()
    expect(screen.getByText('2')).toBeTruthy()
  })

  it('toggles a reaction when clicked', () => {
    const onReact = vi.fn()
    renderList(messages, { reactions, onReact })
    fireEvent.click(screen.getByLabelText(/👍 reaction/))
    expect(onReact).toHaveBeenCalledWith('m1', '👍')
  })
})

describe('MessageList — @mentions + links', () => {
  it('renders an @mention as a chip with the display name', () => {
    const messages = [baseMsg({ body: 'hey <@bob> look' })]
    const roster = [{ accountId: 'bob', displayName: 'Bob Jones', status: 'online' }]
    const { container } = renderList(messages, { roster })
    const mention = container.querySelector('.mention')
    expect(mention).toBeTruthy()
    expect(mention.textContent).toContain('@Bob Jones')
  })

  it('renders a best-effort link preview card for a URL', () => {
    const messages = [baseMsg({ body: 'see https://vulos.org/blog/launch' })]
    renderList(messages)
    expect(screen.getByText('vulos.org')).toBeTruthy()
  })
})

describe('MessageList — author grouping + unread divider', () => {
  it('shows the author name once for consecutive messages by the same author', () => {
    const messages = [
      baseMsg({ id: 'a', body: 'one', created_at: '2025-03-15T10:00:00Z', seq_clock: '00000000000000000001-0000000000-n' }),
      baseMsg({ id: 'b', body: 'two', created_at: '2025-03-15T10:01:00Z', seq_clock: '00000000000000000002-0000000000-n' }),
    ]
    renderList(messages)
    // Author header rendered once (the second message is grouped).
    expect(screen.getAllByText('alice')).toHaveLength(1)
  })

  it('renders a "New" unread divider before the first unread message', () => {
    const messages = [
      baseMsg({ id: 'old', author_id: 'me', body: 'mine', seq_clock: '00000000000000000001-0000000000-n' }),
      baseMsg({ id: 'new', author_id: 'bob', body: 'theirs', seq_clock: '00000000000000000005-0000000000-n' }),
    ]
    renderList(messages, { lastReadClock: '00000000000000000002-0000000000-n' })
    expect(screen.getByTestId('unread-divider')).toBeTruthy()
  })
})
