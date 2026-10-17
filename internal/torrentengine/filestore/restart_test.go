package filestore

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"

	"octopustorrent/internal/files"
	"octopustorrent/internal/testutil/volume"
	"octopustorrent/internal/torrentengine/testutil/fixture"
	"octopustorrent/internal/torrentengine/testutil/swarm"
)

// The restart scenarios run two or three runs of the application in a row over the same save and
// data folders. What a run leaves for the next one is its files.Record, which the tests keep in
// memory; storing it, and writing "not clean" before the first payload write, is task 029's.

// settle waits until the engine has no piece queued, hashing or storing a verdict.
func settle(t *testing.T, tor *torrent.Torrent) {
	t.Helper()
	ctx := deadline(t, transferLimit)
	for {
		busy := false
		for i := range tor.NumPieces() {
			s := tor.PieceState(i)
			busy = busy || s.QueuedForHash || s.Hashing || s.Marking
		}
		if !busy {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("pieces are still being checked")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// badPieces lists the pieces whose bytes on disk do not hash to the torrent's hash, judged without
// the engine: the files are read in torrent order, and a missing or short file is padded with zeros.
func badPieces(t *testing.T, info *metainfo.Info, saveRoot string) []int {
	t.Helper()
	_, paths, err := Layout(saveRoot, info)
	if err != nil {
		t.Fatal(err)
	}
	var stream []byte
	for i, f := range info.UpvertedFiles() {
		data, _ := os.ReadFile(paths[i])
		data = append(data[:min(int64(len(data)), f.Length)], make([]byte, max(f.Length-int64(len(data)), 0))...)
		stream = append(stream, data...)
	}
	var bad []int
	for i := range info.NumPieces() {
		p := info.Piece(i)
		if sha1.Sum(stream[p.Offset():p.Offset()+p.Length()]) != [20]byte(p.V1Hash().Value) {
			bad = append(bad, i)
		}
	}
	return bad
}

func all(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

func without(pieces, remove []int) []int {
	var out []int
	for _, p := range pieces {
		if !slices.Contains(remove, p) {
			out = append(out, p)
		}
	}
	return out
}

func info(t *testing.T, mi *metainfo.MetaInfo) *metainfo.Info {
	t.Helper()
	i, err := mi.UnmarshalInfo()
	if err != nil {
		t.Fatal(err)
	}
	return &i
}

// assertPayload checks that every file of the fixture is below saveRoot with the fixture's bytes.
func assertPayload(t *testing.T, fx fixture.Fixture, saveRoot string) {
	t.Helper()
	for _, f := range fx.Files {
		if got, err := os.ReadFile(fx.FilePath(saveRoot, f)); err != nil || !bytes.Equal(got, fx.Content(f)) {
			t.Errorf("%s is %d bytes (%v), want the fixture's %d", fx.FilePath(saveRoot, f), len(got), err, f.Length)
		}
	}
}

func TestACleanRestartTrustsTheMarksAndReadsNothing(t *testing.T) {
	fx, mi := named(t, "v1-tree")
	seed := seeder(t, fx, mi, swarm.Options{})
	saveRoot, dataDir := t.TempDir(), t.TempDir()

	first := start(t, mi, saveRoot, dataDir, runOptions{})
	first.download(seed)
	first.quitCleanly()

	second := start(t, mi, saveRoot, dataDir, runOptions{prev: &first.record})
	if d := second.decision; d.VerifyAll || len(d.VerifyPieces) != 0 || len(d.Missing) != 0 || len(d.Trusted) != 5 {
		t.Fatalf("decision %+v, want the five files of the torrent trusted and nothing else", d)
	}
	settle(t, second.torrent)
	if got := completePieces(second.torrent); !slices.Equal(got, all(second.torrent.NumPieces())) {
		t.Fatalf("complete pieces after the restart = %v, want all", got)
	}
	second.connect(seed)
	time.Sleep(300 * time.Millisecond)
	if reads, payload := second.storage.Reads(), swarm.Count(second.torrent).PayloadRead; reads != 0 || payload != 0 {
		t.Fatalf("the restart read %d pieces and downloaded %d payload bytes, want none", reads, payload)
	}
	second.quitCleanly()
	if !slices.Equal(first.record.Files, second.record.Files) {
		t.Fatalf("the files were carried over as\n%+v\nwant\n%+v", second.record.Files, first.record.Files)
	}
	assertPayload(t, fx, saveRoot)
}

func TestAPartialDownloadResumesWithoutReadingOrFetchingWhatIsAlreadyThere(t *testing.T) {
	fx, mi := named(t, "v1-large")
	seed := seeder(t, fx, mi, swarm.Options{RateLimit: slowRate})
	saveRoot, dataDir := t.TempDir(), t.TempDir()

	first := start(t, mi, saveRoot, dataDir, runOptions{})
	first.connect(seed)
	waitPieces(t, first.torrent, 20)
	first.torrent.DisallowDataDownload()
	time.Sleep(300 * time.Millisecond)
	have := completePieces(first.torrent)
	first.quitCleanly()

	second := start(t, mi, saveRoot, dataDir, runOptions{prev: &first.record})
	if d := second.decision; d.VerifyAll || len(d.VerifyPieces) != 0 || len(d.Trusted) != 1 {
		t.Fatalf("decision %+v, want the one file trusted", d)
	}
	settle(t, second.torrent)
	if got := completePieces(second.torrent); !slices.Equal(got, have) {
		t.Fatalf("complete pieces after the restart = %v, want the %d the first run had: %v", got, len(have), have)
	}
	if reads := second.storage.Reads(); reads != 0 {
		t.Fatalf("the restart read %d pieces to trust %d marks", reads, len(have))
	}
	second.download(seed)
	want := int64(second.torrent.NumPieces()-len(have)) * 16 << 10
	if got := swarm.Count(second.torrent).PayloadRead; got > want {
		t.Fatalf("the second run downloaded %d payload bytes, want at most the %d missing", got, want)
	}
	second.quitCleanly()
	assertPayload(t, fx, saveRoot)
}

// The child process of the crash test: a leecher that is killed while it works.
func TestLeechChild(t *testing.T) {
	spec := os.Getenv(childEnv)
	if spec == "" {
		return // an ordinary run of the package: the crash test starts this one as a child
	}
	parts := strings.Split(spec, "|")
	if len(parts) != 3 {
		t.Fatalf("%s=%q is not <save folder>|<data folder>|<address>", childEnv, spec)
	}
	// A variable left in the developer's shell must not make this process write anywhere else.
	temp := filepath.Clean(os.TempDir()) + string(filepath.Separator)
	for _, folder := range parts[:2] {
		if !strings.HasPrefix(folder, temp) {
			t.Fatalf("%s names %q, which is not under the temporary folder %s", childEnv, folder, temp)
		}
	}
	leechUntilKilled(t, parts)
}

// leechUntilKilled leeches the torrent from the seeder at addr and prints each piece it completes. It
// never returns: the parent kills the process.
func leechUntilKilled(t *testing.T, parts []string) {
	saveRoot, dataDir, addr := parts[0], parts[1], parts[2]
	_, mi := named(t, "v1-large")
	r := start(t, mi, saveRoot, dataDir, runOptions{})
	if r.addErr != nil {
		t.Fatal(r.addErr)
	}
	tcp, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	r.torrent.AddPeers([]torrent.PeerInfo{{Addr: tcp, Trusted: true}})
	seen := map[int]bool{}
	for {
		for _, p := range completePieces(r.torrent) {
			if !seen[p] {
				seen[p] = true
				fmt.Printf("piece %d\n", p)
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestAKilledProcessLeavesNothingTrustedAndNothingValidIsDownloadedAgain(t *testing.T) {
	fx, mi := named(t, "v1-large")
	seed := seeder(t, fx, mi, swarm.Options{RateLimit: slowRate})
	saveRoot, dataDir := t.TempDir(), t.TempDir()
	lib := libraryIn(t, dataDir)

	cmd := exec.Command(os.Args[0], "-test.run=^TestLeechChild$", "-test.count=1", "-test.timeout=60s")
	cmd.Env = append(os.Environ(), childEnv+"="+strings.Join([]string{saveRoot, dataDir, seed.ListenAddrs()[0].String()}, "|"))
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	keepMoving(t, seed, mi.HashInfoBytes())
	reported := map[int]bool{}
	lines := bufio.NewScanner(stdout)
	for len(reported) < 20 && lines.Scan() {
		if n, ok := strings.CutPrefix(lines.Text(), "piece "); ok {
			p, _ := strconv.Atoi(n)
			reported[p] = true
		}
	}
	if len(reported) < 20 {
		t.Fatalf("the child reported %d pieces: %v", len(reported), lines.Err())
	}
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()

	// No clean quit was recorded: the new run has no record, and verifies everything.
	second := start(t, mi, saveRoot, dataDir, runOptions{})
	if !second.decision.VerifyAll {
		t.Fatalf("decision %+v, want everything verified", second.decision)
	}
	settle(t, second.torrent)
	have := completePieces(second.torrent)
	for p := range reported {
		if !slices.Contains(have, p) {
			t.Errorf("piece %d was complete in the killed process but is not after the restart", p)
		}
	}
	if reads, pieces := second.storage.Reads(), int64(second.torrent.NumPieces()); reads != pieces {
		t.Errorf("the restart read %d pieces, want all %d: nothing may be trusted after a crash", reads, pieces)
	}
	for _, p := range badPieces(t, info(t, mi), saveRoot) {
		if slices.Contains(have, p) {
			t.Errorf("piece %d has wrong bytes on disk but is marked complete", p)
		}
	}
	second.download(seed)
	if got, want := swarm.Count(second.torrent).PayloadRead, int64(second.torrent.NumPieces()-len(have))*16<<10; got > want {
		t.Errorf("downloaded %d payload bytes, want at most the %d missing", got, want)
	}
	second.quitCleanly()
	assertPayload(t, fx, saveRoot)
	lib.intact()
}

// damage is a change made to the file between two runs.
type damage struct {
	name   string
	change func(t *testing.T, path string)
}

func readWrite(t *testing.T, path string, edit func([]byte)) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edit(data)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

var damages = []damage{
	{"truncated by a byte", func(t *testing.T, path string) {
		info, _ := os.Stat(path)
		if err := os.Truncate(path, info.Size()-1); err != nil {
			t.Fatal(err)
		}
	}},
	{"rewritten, same size", func(t *testing.T, path string) { readWrite(t, path, func(b []byte) { b[len(b)/2] ^= 0xff }) }},
	{"rewritten, same size and mtime", func(t *testing.T, path string) {
		info, _ := os.Stat(path)
		readWrite(t, path, func(b []byte) { b[len(b)/2] ^= 0xff })
		if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
			t.Fatal(err)
		}
	}},
	{"replaced by an identical copy", func(t *testing.T, path string) {
		data, _ := os.ReadFile(path)
		copyPath := path + ".copy"
		if err := os.WriteFile(copyPath, data, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(copyPath, path); err != nil {
			t.Fatal(err)
		}
	}},
}

func TestEveryChangedFileIsVerifiedAndNoOtherPieceIsRead(t *testing.T) {
	for _, d := range damages {
		t.Run(d.name, func(t *testing.T) {
			fx, mi := named(t, "v1-tree")
			seed := seeder(t, fx, mi, swarm.Options{})
			saveRoot, dataDir := t.TempDir(), t.TempDir()
			first := start(t, mi, saveRoot, dataDir, runOptions{})
			first.download(seed)
			first.quitCleanly()

			time.Sleep(20 * time.Millisecond) // keep the changes apart from the downloads in time
			d.change(t, fx.FilePath(saveRoot, fx.Files[0]))

			second := start(t, mi, saveRoot, dataDir, runOptions{prev: &first.record})
			if dec := second.decision; dec.VerifyAll || len(dec.Missing) != 0 || !slices.Equal(dec.VerifyPieces, []int{0}) || len(dec.Trusted) != 4 {
				t.Fatalf("decision %+v, want piece 0 (the changed file) verified and the other four files trusted", dec)
			}
			settle(t, second.torrent)
			bad := badPieces(t, second.torrent.Info(), saveRoot)
			if got, want := completePieces(second.torrent), without(all(second.torrent.NumPieces()), bad); !slices.Equal(got, want) {
				t.Fatalf("complete pieces = %v, want %v (bad pieces: %v)", got, want, bad)
			}
			if reads := second.storage.Reads(); reads != 1 {
				t.Fatalf("%d pieces were read, want only the one verified", reads)
			}
			second.download(seed)
			if got, want := swarm.Count(second.torrent).PayloadRead, int64(len(bad))*32<<10; got > want {
				t.Errorf("downloaded %d payload bytes, want at most the %d of the damaged pieces", got, want)
			}
			second.quitCleanly()
			assertPayload(t, fx, saveRoot)
		})
	}
}

func TestAFileThatWasMovedAwayIsReportedMissingAndNothingIsAdded(t *testing.T) {
	fx, mi := named(t, "v1-tree")
	seed := seeder(t, fx, mi, swarm.Options{})
	saveRoot, dataDir := t.TempDir(), t.TempDir()
	first := start(t, mi, saveRoot, dataDir, runOptions{})
	first.download(seed)
	first.quitCleanly()

	victim := fx.FilePath(saveRoot, fx.Files[0])
	if err := os.Rename(victim, victim+".moved"); err != nil {
		t.Fatal(err)
	}
	d, _ := decide(t, mi, saveRoot, &first.record)
	if d.VerifyAll || !slices.Equal(d.Missing, []string{victim}) || len(d.VerifyPieces) != 0 {
		t.Fatalf("decision %+v, want exactly the moved file missing", d)
	}
	if _, err := os.Stat(victim); err == nil {
		t.Fatal("deciding recreated the file")
	}
}

func TestAFileMovedBeforeACrashIsStillMissingAndNothingIsDownloadedInItsPlace(t *testing.T) {
	fx, mi := named(t, "v1-tree")
	seed := seeder(t, fx, mi, swarm.Options{})
	saveRoot, dataDir := t.TempDir(), t.TempDir()
	first := start(t, mi, saveRoot, dataDir, runOptions{})
	first.download(seed)
	first.quitCleanly()

	// the next run began (its record is "not clean": 029 writes that before the first payload write),
	// the owner moved a file, and the process was killed
	crashed := first.record
	crashed.Clean = false
	victim := fx.FilePath(saveRoot, fx.Files[0])
	if err := os.Rename(victim, victim+".moved"); err != nil {
		t.Fatal(err)
	}
	d, _ := decide(t, mi, saveRoot, &crashed)
	if !d.VerifyAll || !slices.Equal(d.Missing, []string{victim}) {
		t.Fatalf("decision %+v, want everything verified and the moved file missing", d)
	}
	if _, err := os.Stat(victim); err == nil {
		t.Fatal("deciding recreated the file")
	}
}

// Engine fact (finding 20): the engine believes a stored mark over a file of the right size and
// does not hash. It cannot see a same-size rewrite, and it re-downloads, without hashing, a file that
// is short or gone. This is why a mark is never trusted on its own.
func TestTrustedMarksAreBlindToASameSizeRewrite(t *testing.T) {
	fx, mi := named(t, "v1-tree")
	seed := seeder(t, fx, mi, swarm.Options{})
	saveRoot, dataDir := t.TempDir(), t.TempDir()
	first := start(t, mi, saveRoot, dataDir, runOptions{})
	first.download(seed)
	first.quitCleanly()
	readWrite(t, fx.FilePath(saveRoot, fx.Files[0]), func(b []byte) { b[len(b)/2] ^= 0xff })

	believing := start(t, mi, saveRoot, dataDir, runOptions{trustMarks: true})
	settle(t, believing.torrent)
	n := believing.torrent.NumPieces()
	if bad := badPieces(t, believing.torrent.Info(), saveRoot); !slices.Equal(bad, []int{0}) {
		t.Fatalf("bad pieces = %v, want piece 0", bad)
	}
	if got := completePieces(believing.torrent); !slices.Equal(got, all(n)) {
		t.Fatalf("complete pieces = %v: the engine no longer believes the marks over a same-size rewrite", got)
	}
	if reads := believing.storage.Reads(); reads != 0 {
		t.Fatalf("%d pieces were read", reads)
	}
	believing.quit()
}

// An outside edit made while the application runs is not taken for the run's own work: the next
// start sees the earlier fingerprint, which no longer matches.
func TestAnEditMadeWhileTheApplicationRunsIsVerifiedAtTheNextStart(t *testing.T) {
	fx, mi := named(t, "v1-tree")
	seed := seeder(t, fx, mi, swarm.Options{})
	saveRoot, dataDir := t.TempDir(), t.TempDir()
	first := start(t, mi, saveRoot, dataDir, runOptions{})
	first.download(seed)
	first.quitCleanly()

	second := start(t, mi, saveRoot, dataDir, runOptions{prev: &first.record})
	settle(t, second.torrent)
	if len(second.decision.VerifyPieces) != 0 || second.storage.Reads() != 0 {
		t.Fatalf("the second run did not trust the files: %+v", second.decision)
	}
	time.Sleep(20 * time.Millisecond)
	readWrite(t, fx.FilePath(saveRoot, fx.Files[0]), func(b []byte) { b[len(b)/2] ^= 0xff }) // while it runs
	second.quitCleanly()
	if second.record.Files[0].Fingerprint != first.record.Files[0].Fingerprint {
		t.Fatal("the edited file's fingerprint was refreshed instead of carried over")
	}

	third := start(t, mi, saveRoot, dataDir, runOptions{prev: &second.record})
	if !slices.Equal(third.decision.VerifyPieces, []int{0}) {
		t.Fatalf("decision %+v, want piece 0 verified", third.decision)
	}
	settle(t, third.torrent)
	if got, want := completePieces(third.torrent), []int{1, 2}; !slices.Equal(got, want) {
		t.Fatalf("complete pieces = %v, want %v", got, want)
	}
	third.quit()
}

func TestOnlyAPFSVolumesAreTrustedAndAReplugIsNotAMissingFile(t *testing.T) {
	fx, mi := named(t, "v1-single")
	seed := seeder(t, fx, mi, swarm.Options{})
	for _, tc := range []struct{ fs, name, label string }{
		{"MS-DOS", "msdos", "OctoFAT"}, {"ExFAT", "exfat", "OctoExFAT"}, {"APFS", "apfs", "OctoAPFS"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vol := volume.New(t, volume.Options{Size: "8m", FS: tc.fs, Name: tc.label})
			saveRoot := filepath.Join(vol.Mount, "Downloads")
			if err := os.Mkdir(saveRoot, 0o755); err != nil {
				t.Fatal(err)
			}
			dataDir := t.TempDir()
			first := start(t, mi, saveRoot, dataDir, runOptions{})
			first.download(seed)
			first.quitCleanly()
			if first.record.FileSystem != tc.name {
				t.Fatalf("the record says %q, want %q", first.record.FileSystem, tc.name)
			}

			second := start(t, mi, saveRoot, dataDir, runOptions{prev: &first.record})
			if tc.name == "apfs" {
				if second.decision.VerifyAll || second.storage.Reads() != 0 {
					t.Fatalf("an APFS volume was not trusted: %+v", second.decision)
				}
				second.quitCleanly()

				// pulled and plugged again, with another volume taking its device number
				replug(t, vol, saveRoot, second.record.Dev)
				d, _ := decide(t, mi, saveRoot, &second.record)
				if !d.VerifyAll || len(d.Missing) != 0 || !strings.Contains(d.Reason, "another volume") {
					t.Fatalf("after a replug the decision is %+v, want everything verified and nothing missing", d)
				}
				return
			}
			if !second.decision.VerifyAll || !strings.Contains(second.decision.Reason, "reliable") {
				t.Fatalf("decision %+v, want everything verified", second.decision)
			}
			settle(t, second.torrent)
			if got := completePieces(second.torrent); !slices.Equal(got, all(second.torrent.NumPieces())) {
				t.Fatalf("complete pieces = %v", got)
			}
			if reads := second.storage.Reads(); reads != int64(second.torrent.NumPieces()) {
				t.Fatalf("%d pieces were read, want all %d", reads, second.torrent.NumPieces())
			}
			second.quitCleanly()
		})
	}
}

// Engine fact (finding 21): on HFS+ the engine's classic file IO cannot hash a piece, because it
// seeks with SEEK_DATA, which HFS+ lacks. Every piece fails its check and the download never
// completes. The guard refuses such a save folder; this test shows what happens without it.
func TestOnHFSPlusTheEngineCannotHashAnyPiece(t *testing.T) {
	fx, mi := named(t, "v1-single")
	seed := seeder(t, fx, mi, swarm.Options{})
	vol := volume.New(t, volume.Options{Size: "8m", FS: "HFS+", Name: "OctoHFS"})
	plain := plainStorage(vol.Mount, storage.NewMapPieceCompletion())
	p, err := swarm.NewPeer(t.TempDir(), swarm.Options{DefaultStorage: Refusing(), DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close(deadline(t, closeLimit)); _ = plain.Close() })
	tor, _ := p.Client().AddTorrentOpt(torrent.AddTorrentOpts{InfoHash: mi.HashInfoBytes(), Storage: plain})
	if err := tor.SetInfoBytes(mi.InfoBytes); err != nil {
		t.Fatal(err)
	}
	tor.DownloadAll()
	connectPlain(t, tor, seed)
	time.Sleep(2 * time.Second)
	if n := len(completePieces(tor)); n != 0 {
		t.Fatalf("%d pieces completed on HFS+: the engine can hash there now (finding 21)", n)
	}
	if !strings.Contains(p.Logs(), "inappropriate ioctl for device") {
		t.Fatalf("the engine's log does not show the SEEK_DATA failure:\n%s", p.Logs())
	}
}

// replug detaches and attaches a volume again until it gets another device number than before; the
// system hands out the number it just freed unless another volume takes it first.
func replug(t *testing.T, vol *volume.Volume, saveRoot string, before uint64) {
	t.Helper()
	for attempt := range 4 {
		vol.Detach()
		volume.New(t, volume.Options{Size: "8m", FS: "APFS", Name: fmt.Sprintf("OctoOther%d", attempt)})
		vol.Attach()
		if now, err := files.Stat(saveRoot); err == nil && now.Dev != before {
			return
		}
	}
	t.Fatal("the volume kept its device number through four replugs")
}
