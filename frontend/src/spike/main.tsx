import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { SpikeApp } from './SpikeApp'
import { spikeBridge } from './bridge'

const container = document.getElementById('root')
if (container === null) {
  throw new Error('Root element #root not found')
}

createRoot(container).render(
  <StrictMode>
    <SpikeApp bridge={spikeBridge} />
  </StrictMode>,
)
