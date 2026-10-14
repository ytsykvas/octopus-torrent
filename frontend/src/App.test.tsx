import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import App from './App'

vi.mock('./bridge/runtimeInfo', () => ({
  getRuntimeInfo: () => Promise.resolve({ goVersion: 'go1.27.2', os: 'darwin', arch: 'arm64' }),
}))

it('wires the runtime check to the Wails bridge', async () => {
  render(<App />)

  expect(screen.getByRole('heading', { level: 1, name: 'OctopusTorrent' })).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Check Go connection' }))

  expect(await screen.findByText('Go go1.27.2 · darwin/arm64')).toBeInTheDocument()
})
