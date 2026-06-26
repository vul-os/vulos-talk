/**
 * bots.test.jsx — Apps & Bots admin UI.
 * Mocks the api client; verifies list/empty rendering and the create flow that
 * surfaces the one-time token + signing secret panel.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

vi.mock('../../lib/api.js', () => ({
  api: {
    botsList: vi.fn(),
    botCreate: vi.fn(),
    botDelete: vi.fn(),
    botRotateToken: vi.fn(),
    botRotateSecret: vi.fn(),
    botUpdate: vi.fn(),
  },
}))

import BotsApp from './BotsApp.jsx'
import { api } from '../../lib/api.js'

function renderApp() {
  return render(<MemoryRouter><BotsApp /></MemoryRouter>)
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('BotsApp', () => {
  it('shows an empty state when there are no bots', async () => {
    api.botsList.mockResolvedValue([])
    renderApp()
    await waitFor(() => expect(screen.getByText('No bots yet')).toBeTruthy())
  })

  it('degrades gracefully when the bots route 404s', async () => {
    api.botsList.mockRejectedValue(Object.assign(new Error('not found'), { status: 404 }))
    renderApp()
    await waitFor(() => expect(screen.getByText('No bots yet')).toBeTruthy())
  })

  it('lists existing bots with their scopes', async () => {
    api.botsList.mockResolvedValue([
      { id: 'b1', name: 'deploy-bot', scopes: ['chat:write', 'history:read'], slash_commands: [] },
    ])
    renderApp()
    await waitFor(() => expect(screen.getByText('deploy-bot')).toBeTruthy())
    expect(screen.getByText('chat:write')).toBeTruthy()
    expect(screen.getByText('history:read')).toBeTruthy()
  })

  it('creates a bot and reveals the one-time token + signing secret', async () => {
    api.botsList.mockResolvedValue([])
    api.botCreate.mockResolvedValue({
      bot: { id: 'b9', name: 'ci-bot', scopes: ['chat:write'] },
      token: 'xoxb-secret-token',
      signing_secret: 'sign-secret-123',
      incoming_webhook_url: 'https://talk.example/api/hooks/b9',
    })
    renderApp()
    await waitFor(() => expect(screen.getByText('No bots yet')).toBeTruthy())

    // Open the create modal (header button).
    fireEvent.click(screen.getAllByText('Create bot')[0])
    const nameInput = await screen.findByPlaceholderText('e.g. deploy-bot')
    fireEvent.change(nameInput, { target: { value: 'ci-bot' } })

    await act(async () => {
      const createButtons = screen.getAllByText('Create bot')
      fireEvent.click(createButtons[createButtons.length - 1]) // modal submit
    })

    await waitFor(() => expect(api.botCreate).toHaveBeenCalled())
    expect(screen.getByText('xoxb-secret-token')).toBeTruthy()
    expect(screen.getByText('sign-secret-123')).toBeTruthy()
    expect(screen.getByText('https://talk.example/api/hooks/b9')).toBeTruthy()
  })
})
