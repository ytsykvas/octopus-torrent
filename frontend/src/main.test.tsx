import { act, screen } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'

vi.mock('./App', () => ({ default: () => <p>app mounted</p> }))

beforeEach(() => {
  vi.resetModules()
  document.body.innerHTML = ''
})

it('mounts the application into #root', async () => {
  document.body.innerHTML = '<div id="root"></div>'

  await act(async () => {
    await import('./main')
  })

  expect(screen.getByText('app mounted')).toBeInTheDocument()
})

it('fails loudly when #root is missing', async () => {
  await expect(import('./main')).rejects.toThrow('Root element #root not found')
})
