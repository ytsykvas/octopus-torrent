import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import type { Result } from '../../bridge/contract/errors'
import type { RuntimeInfo } from '../../bridge/contract/runtimeInfo'
import { RuntimeCheck } from './RuntimeCheck'

const info: RuntimeInfo = { goVersion: 'go1.0', os: 'darwin', arch: 'arm64', sqliteVersion: '3.53.4' }
const answered: Result<RuntimeInfo> = { ok: true, value: info }

describe('RuntimeCheck', () => {
  it('starts with an unchecked status and does not call Go', () => {
    const load = vi.fn<() => Promise<Result<RuntimeInfo>>>(() => Promise.resolve(answered))
    render(<RuntimeCheck loadRuntimeInfo={load} />)

    expect(screen.getByRole('status')).toHaveTextContent('Not checked yet')
    expect(load).not.toHaveBeenCalled()
  })

  it('shows progress, then the Go runtime answer', async () => {
    const pending: { resolve?: (value: Result<RuntimeInfo>) => void } = {}
    const load = vi.fn<() => Promise<Result<RuntimeInfo>>>(
      () =>
        new Promise((resolve) => {
          pending.resolve = resolve
        }),
    )
    render(<RuntimeCheck loadRuntimeInfo={load} />)

    await userEvent.click(screen.getByRole('button', { name: 'Check Go connection' }))
    expect(screen.getByRole('status')).toHaveTextContent('Checking…')

    pending.resolve?.(answered)
    expect(await screen.findByText('Go go1.0 · darwin/arm64 · SQLite 3.53.4')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('works from the keyboard and keeps focus on the button', async () => {
    render(<RuntimeCheck loadRuntimeInfo={() => Promise.resolve(answered)} />)

    await userEvent.tab()
    const button = screen.getByRole('button', { name: 'Check Go connection' })
    expect(button).toHaveFocus()
    await userEvent.keyboard('{Enter}')

    expect(await screen.findByText('Go go1.0 · darwin/arm64 · SQLite 3.53.4')).toBeInTheDocument()
    expect(button).toHaveFocus()
  })

  it('shows the message of a failed call as an alert, without its diagnostic text', async () => {
    const failed: Result<RuntimeInfo> = {
      ok: false,
      error: { code: 'bridge.callFailed', operationID: null, torrentID: null, params: { detail: '/Users/x/private' } },
    }
    render(<RuntimeCheck loadRuntimeInfo={() => Promise.resolve(failed)} />)

    await userEvent.click(screen.getByRole('button', { name: 'Check Go connection' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('The application did not answer.')
    expect(screen.getByRole('alert')).not.toHaveTextContent('/Users/x/private')
    expect(screen.getByRole('status')).toBeEmptyDOMElement()
  })
})
