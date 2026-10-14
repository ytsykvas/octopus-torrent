import { getRuntimeInfo } from './bridge/runtimeInfo'
import { RuntimeCheck } from './features/runtime-check/RuntimeCheck'

export default function App() {
  return (
    <main>
      <h1>OctopusTorrent</h1>
      <RuntimeCheck loadRuntimeInfo={getRuntimeInfo} />
    </main>
  )
}
