// Package transfer decides what a torrent may transfer and what its card says, and keeps that
// state for every torrent of the running application (TR-01, TR-03, TR-08, STACK-03).
//
// Decide and DisplayStatus are pure: the same inputs give the same answer, with no engine, disk or
// network. Registry owns the inputs of every torrent in this runtime and applies the answer through
// the engine's switches in each torrent's command order. The tables, the reasons for each row and
// the rules for callers are in the ADR transfer-status-model.
package transfer

// Phase is the work the application is doing on a torrent's record. Only a ready torrent may
// transfer payload; every other phase stops both directions while it lasts. The zero value is no
// phase: a caller that forgets to set it gets a torrent that never transfers, and Add refuses it.
type Phase uint8

const (
	// PhaseReady: the record has its metadata and its data location, and nothing else runs on it;
	// whether it transfers is the policy's decision.
	PhaseReady Phase = iota + 1
	// PhaseWaitingForMetadata: the info of a magnet is not known yet. A library record is created
	// only after verified metadata (011, 022, 023), so no record is in this phase today; the rule
	// exists for one that ever is.
	PhaseWaitingForMetadata
	// PhaseChecking: the application is verifying the data on disk (029, 030). A check is not
	// transfer (TR-01), and seeding waits for it.
	PhaseChecking
	// PhaseRelinking: the user is choosing a new location for missing data (030).
	PhaseRelinking
	// PhaseRemoving: the removal of the record has started (031, 032). It never ends in another
	// phase: the record is either removed or stays stopped.
	PhaseRemoving
)

func (p Phase) valid() bool { return p >= PhaseReady && p <= PhaseRemoving }

// Input is everything the policy depends on for one torrent at one moment.
type Input struct {
	// ManualPaused is the user's stored pause (TR-01): no payload in either direction.
	ManualPaused bool
	// GlobalUploadDisabled is the stored "stop upload for all torrents" (TR-08): no upload, and
	// download is not touched.
	GlobalUploadDisabled bool
	Phase                Phase
	// PayloadFault means the torrent's files cannot be used safely: the folder is missing, replaced,
	// unreadable or full, or deleting them failed. A failed or unconfirmed apply, a network or a
	// tracker problem is not a fault: it is reported, and never stops the other direction.
	PayloadFault bool
	// ShuttingDown is set once the application quits: payload stops without changing the stored
	// intent, so the next start restores it.
	ShuttingDown bool
}

// Permissions are the two payload switches of one torrent, computed independently: piece data only;
// metadata, haves and keep-alives are protocol traffic and never counted as transfer.
type Permissions struct {
	Download bool
	Upload   bool
}

// Decide returns the payload permissions for in. Wanted files and completion are not inputs: which
// files are wanted is their priority in the engine (TR-06), and download stays allowed when
// everything wanted is complete, so a newly selected file needs no switch call.
func Decide(in Input) Permissions {
	if in.ShuttingDown || in.Phase != PhaseReady || in.PayloadFault || in.ManualPaused {
		return Permissions{}
	}
	return Permissions{Download: true, Upload: !in.GlobalUploadDisabled}
}
