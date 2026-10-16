// Package enginespike is the service of the temporary engine spike app (cmd/engine-spike, task 007;
// task 034 deletes both). It moves real bytes between two loopback engine clients of the test swarm
// and publishes the transfer as its own DTO, so the spike proves the path engine → Go DTO → Wails
// binding and event → React before the product's engine adapter (018) exists. It is a test
// instrument: it reuses the swarm and fixtures of internal/torrentengine/testutil, and the product
// binary never links it (the gate's product isolation step).
package enginespike

import "octopustorrent/internal/app/contracts"

// Event is the name of the Wails event that carries every Transfer the service publishes.
const Event = "enginespike:transfer"

// State is where the spike's transfer is.
type State string

const (
	Idle     State = "idle"     // nothing started yet
	Running  State = "running"  // the seeder uploads, the leecher downloads
	Paused   State = "paused"   // the seeder's upload is disallowed (not the product's pause policy)
	Complete State = "complete" // every byte arrived; nothing is published after this
	Failed   State = "failed"   // Start failed and released what it had created; Start may be retried
)

// Transfer is one published state of the spike. Revision orders them: every command and every tick
// publishes the next revision, and the frontend keeps only the highest it has seen, so a slow reply
// never puts an older state back on the screen.
type Transfer struct {
	Revision       contracts.Revision  `json:"revision" ts_type:"string"`
	State          State               `json:"state"`
	BytesCompleted contracts.ByteCount `json:"bytesCompleted" ts_type:"string"` // verified bytes at the leecher
	Length         contracts.ByteCount `json:"length" ts_type:"string"`
	PayloadRead    contracts.ByteCount `json:"payloadRead" ts_type:"string"`    // piece data the leecher received
	PayloadWritten contracts.ByteCount `json:"payloadWritten" ts_type:"string"` // piece data the leecher sent
	Peers          int                 `json:"peers"`
	// ClientsStarted counts every engine client this service created; a frontend reload must not
	// change it.
	ClientsStarted int    `json:"clientsStarted"`
	Error          string `json:"error"`
}
