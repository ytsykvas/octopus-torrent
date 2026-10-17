import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import App from './App'

vi.mock('./bridge/runtimeInfo', () => ({
  getRuntimeInfo: () =>
    Promise.resolve({ ok: true, value: { goVersion: 'go1.0', os: 'darwin', arch: 'arm64', sqliteVersion: '3.53.4' } }),
}))

it('wires the runtime check to the Wails bridge', async () => {
  render(<App />)

  expect(screen.getByRole('heading', { level: 1, name: 'OctopusTorrent' })).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Check Go connection' }))

  expect(await screen.findByText('Go go1.0 · darwin/arm64 · SQLite 3.53.4')).toBeInTheDocument()
})
