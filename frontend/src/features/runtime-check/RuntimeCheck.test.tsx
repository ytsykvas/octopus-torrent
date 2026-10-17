import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import type { RuntimeInfo } from '../../bridge/runtimeInfo'
import { RuntimeCheck } from './RuntimeCheck'

const info: RuntimeInfo = { goVersion: 'go1.27.2', os: 'darwin', arch: 'arm64', sqliteVersion: '3.53.4' }

describe('RuntimeCheck', () => {
  it('starts with an unchecked status and does not call Go', () => {
    const load = vi.fn<() => Promise<RuntimeInfo>>(() => Promise.resolve(info))
    render(<RuntimeCheck loadRuntimeInfo={load} />)

    expect(screen.getByRole('status')).toHaveTextContent('Not checked yet')
    expect(load).not.toHaveBeenCalled()
  })

  it('shows progress, then the Go runtime answer', async () => {
    const pending: { resolve?: (value: RuntimeInfo) => void } = {}
    const load = vi.fn<() => Promise<RuntimeInfo>>(
      () =>
        new Promise((resolve) => {
          pending.resolve = resolve
        }),
    )
    render(<RuntimeCheck loadRuntimeInfo={load} />)

    await userEvent.click(screen.getByRole('button', { name: 'Check Go connection' }))
    expect(screen.getByRole('status')).toHaveTextContent('Checking…')

    pending.resolve?.(info)
    expect(await screen.findByText('Go go1.27.2 · darwin/arm64 · SQLite 3.53.4')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('works from the keyboard and keeps focus on the button', async () => {
    render(<RuntimeCheck loadRuntimeInfo={() => Promise.resolve(info)} />)

    await userEvent.tab()
    const button = screen.getByRole('button', { name: 'Check Go connection' })
    expect(button).toHaveFocus()
    await userEvent.keyboard('{Enter}')

    expect(await screen.findByText('Go go1.27.2 · darwin/arm64 · SQLite 3.53.4')).toBeInTheDocument()
    expect(button).toHaveFocus()
  })

  it('reports an Error from Go as an alert', async () => {
    render(<RuntimeCheck loadRuntimeInfo={() => Promise.reject(new Error('binding failed'))} />)

    await userEvent.click(screen.getByRole('button', { name: 'Check Go connection' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Go call failed: binding failed')
  })

  it('reports a non-Error rejection as text', async () => {
    render(<RuntimeCheck loadRuntimeInfo={() => Promise.reject('no window.go')} />)

    await userEvent.click(screen.getByRole('button', { name: 'Check Go connection' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Go call failed: no window.go')
  })
})
