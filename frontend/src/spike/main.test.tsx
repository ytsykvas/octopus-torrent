import { act, screen } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'

vi.mock('./SpikeApp', () => ({ SpikeApp: ({ bridge }: { bridge: unknown }) => <p>spike mounted {typeof bridge}</p> }))

beforeEach(() => {
  vi.resetModules()
  document.body.innerHTML = ''
})

it('mounts the spike with the real bridge into #root', async () => {
  document.body.innerHTML = '<div id="root"></div>'

  await act(async () => {
    await import('./main')
  })

  expect(screen.getByText('spike mounted object')).toBeInTheDocument()
})

it('fails loudly when #root is missing', async () => {
  await expect(import('./main')).rejects.toThrow('Root element #root not found')
})
