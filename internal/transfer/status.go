package transfer

import "octopustorrent/internal/app/contracts"

// Progress is what the engine and the file selection observe about a torrent's data.
type Progress struct {
	// WantedFiles is the number of selected real files (pad files excluded).
	WantedFiles int
	// Complete is true when every selected file is verified.
	Complete bool
}

// DisplayStatus returns the one status the card and the details show (UI-01, UI-08, UI-19). The
// first condition that holds wins, in this order: relinking, a payload fault, removing, checking,
// waiting for metadata, an unknown phase (shown as an error), manual pause, no wanted file, missing
// data, then seeding when the global upload stop is off, else completed. Seeding follows the policy,
// never the engine's own seeding flag or the observed rate. Shutdown does not change the status.
func DisplayStatus(in Input, p Progress) contracts.TorrentStatus {
	switch {
	case in.Phase == PhaseRelinking:
		return contracts.StatusRelinking
	case in.PayloadFault:
		return contracts.StatusError
	case in.Phase == PhaseRemoving:
		return contracts.StatusDeleting
	case in.Phase == PhaseChecking:
		return contracts.StatusChecking
	case in.Phase == PhaseWaitingForMetadata:
		return contracts.StatusWaitingForMetadata
	case in.Phase != PhaseReady:
		return contracts.StatusError
	case in.ManualPaused:
		return contracts.StatusPaused
	case p.WantedFiles == 0:
		return contracts.StatusNoFilesSelected
	case !p.Complete:
		return contracts.StatusDownloading
	case !in.GlobalUploadDisabled:
		return contracts.StatusSeeding
	default:
		return contracts.StatusCompleted
	}
}
