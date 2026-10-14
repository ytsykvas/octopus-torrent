import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import type { RuntimeInfo } from '../../bridge/runtimeInfo'
import { RuntimeCheck } from './RuntimeCheck'

const info: RuntimeInfo = { goVersion: 'go1.27.2', os: 'darwin', arch: 'arm64' }

describe('RuntimeCheck', () => {
  it('starts with an unchecked status and does not call Go', () => {
    const load = vi.fn(() => Promise.resolve(info))
    render(<RuntimeCheck loadRuntimeInfo={load} />)

    expect(screen.getByRole('status')).toHaveTextContent('Not checked yet')
    expect(load).not.toHaveBeenCalled()
  })

  it('shows progress, then the Go runtime answer', async () => {
    let resolve: (value: RuntimeInfo) => void = () => undefined
    const load = vi.fn(() => new Promise<RuntimeInfo>((r) => { resolve = r }))
    render(<RuntimeCheck loadRuntimeInfo={load} />)

    await userEvent.click(screen.getByRole('button', { name: 'Check Go connection' }))
    expect(screen.getByRole('status')).toHaveTextContent('Checking…')

    resolve(info)
    expect(await screen.findByText('Go go1.27.2 · darwin/arm64')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('works from the keyboard and keeps focus on the button', async () => {
    render(<RuntimeCheck loadRuntimeInfo={() => Promise.resolve(info)} />)

    await userEvent.tab()
    const button = screen.getByRole('button', { name: 'Check Go connection' })
    expect(button).toHaveFocus()
    await userEvent.keyboard('{Enter}')

    expect(await screen.findByText('Go go1.27.2 · darwin/arm64')).toBeInTheDocument()
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
