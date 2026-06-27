/**
 * bots.test.jsx — Talk's Apps & Bots manage surface.
 *
 * After the migration, BotsApp is a thin host around the shared @vulos/apps-ui
 * <AppsAndBots/> component in product mode. We mock the library and assert
 * BotsApp mounts it against Talk's /api/apps surface with a cookie-authed
 * fetcher, and that the back-to-Talk affordance renders.
 */
import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

// Capture the props AppsAndBots is mounted with.
const appsAndBotsProps = []
vi.mock('@vulos/apps-ui', () => ({
  default: (props) => {
    appsAndBotsProps.push(props)
    return <div data-testid="apps-and-bots">apps-and-bots</div>
  },
}))
vi.mock('@vulos/apps-ui/styles.css', () => ({}))
vi.mock('@vulos/relay-client/endpoints', () => ({
  selectEndpoint: vi.fn(async () => ''),
}))

import BotsApp from './BotsApp.jsx'

function renderApp() {
  return render(<MemoryRouter><BotsApp /></MemoryRouter>)
}

describe('BotsApp', () => {
  it('mounts <AppsAndBots/> in product mode against /api/apps', () => {
    renderApp()
    expect(screen.getByTestId('apps-and-bots')).toBeTruthy()
    const props = appsAndBotsProps.at(-1)
    expect(props.mode).toBe('product')
    expect(props.product).toBe('talk')
    expect(props.basePath).toBe('/api/apps')
    expect(typeof props.fetcher).toBe('function')
  })

  it('renders a back-to-Talk control', () => {
    renderApp()
    expect(screen.getByTitle('Back to Talk')).toBeTruthy()
  })

  it('its fetcher sends credentials for cookie auth', async () => {
    renderApp()
    const { fetcher } = appsAndBotsProps.at(-1)
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response('[]', { status: 200 }),
    )
    await fetcher('/api/apps', { method: 'GET' })
    expect(fetchSpy).toHaveBeenCalledWith(
      '/api/apps',
      expect.objectContaining({ credentials: 'include' }),
    )
    fetchSpy.mockRestore()
  })
})
