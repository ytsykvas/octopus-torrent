package devtool

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"octopustorrent/internal/datadir"
)

// The data folders of the tooling, relative to the repository root (task 014). Every one is given to
// the application through OCTOPUS_DATA_DIR, so nothing the tooling starts opens the owner's library.
const (
	// workspaceData is the data folder of `workspace run` (wails dev) in a worktree.
	workspaceData = "build/workspace-data"
	// toolData is the data folder of every Wails command of the gate (bindings, builds). No build
	// opens a library, so it stays empty; a library there means the build-mode tags failed.
	toolData = "build/tool-data"
	// appDataRoot holds the data folder of each .app the gate runs, one per app.
	appDataRoot = coverRoot + "/app-data"
)

// dataEnv is the OCTOPUS_DATA_DIR assignment of the folder rel under root.
func dataEnv(root, rel string) string {
	return datadir.Override + "=" + filepath.Join(root, rel)
}

// ownerData describes the owner's data folder: the folder's own modification time (a file created
// and removed again, such as a library's WAL, changes it) and each entry's size and modification
// time, keyed by path. Finder's .DS_Store is left out. A missing folder is an empty description.
func ownerData(sys System) (map[string]string, error) {
	home, err := sys.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := datadir.Default(home)
	info, err := sys.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	described := map[string]string{dir: "folder modified " + stamp(info.ModTime())}
	names, err := sys.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		if name == ".DS_Store" {
			continue
		}
		path := filepath.Join(dir, name)
		entry, err := sys.Lstat(path)
		if err != nil {
			return nil, err
		}
		described[path] = fmt.Sprintf("%d bytes, modified %s", entry.Size(), stamp(entry.ModTime()))
	}
	return described, nil
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// dataFolders is the gate's last step: the owner's data folder is as the start found it, and no
// Wails command of the gate opened a library in the tool folder.
func (c *checker) dataFolders() stepResult {
	var problems []string
	after, err := ownerData(c.sys)
	switch {
	case c.ownerErr != nil:
		problems = append(problems, "the owner's data folder could not be read at the start: "+c.ownerErr.Error())
	case err != nil:
		problems = append(problems, "the owner's data folder could not be read at the end: "+err.Error())
	default:
		if changed := changedNames(c.ownerBefore, after); len(changed) > 0 {
			problems = append(problems, "the owner's data folder changed during the check (a step of this run opened it, or the owner's OctopusTorrent ran meanwhile): "+strings.Join(changed, ", "))
		}
	}
	// The start removed the tool folder, so a missing folder is the expected answer.
	switch names, err := c.sys.ReadDir(filepath.Join(c.root, toolData)); {
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		problems = append(problems, toolData+" could not be read: "+err.Error())
	case len(names) > 0:
		problems = append(problems, "a build opened a library in "+toolData+": "+strings.Join(names, ", "))
	}
	if len(problems) > 0 {
		return fail(fmt.Sprintf("%d problems", len(problems)), strings.Join(problems, "\n"))
	}
	return pass("the owner's data folder did not change, and no build opened a library")
}
