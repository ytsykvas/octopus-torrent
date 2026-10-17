package filestore

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"

	"octopustorrent/internal/files"
)

// ErrNoSaveFolder is returned by the default storage of the engine client: a torrent added without
// storage of its own has no folder the user chose, and nothing is written anywhere.
var ErrNoSaveFolder = errors.New("the torrent has no save folder: it needs storage of its own")

// NewTorrentStorage is the storage for one torrent: the engine's file storage with the payload under
// saveRoot, part files off, and the given completion, inside a guard. The engine's file storage logs
// absolute paths; a nil logger discards them. Seal requires the completion to be closed and keeps
// files with unresolved pieces out of the record. completion must not be nil.
func NewTorrentStorage(saveRoot string, completion *Completion, session *Session, logger *slog.Logger) (*TorrentStorage, error) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	opts := storage.NewFileClientOpts{
		ClientBaseDir:   saveRoot,
		FilePathMaker:   filePath,
		PieceCompletion: completion,
		Logger:          logger,
	}
	opts.UsePartFiles.Set(false)
	return newGuard(storage.NewFileOpts(opts), saveRoot, session, completion)
}

// filePath is the engine's own default rule for the path of a file below the save folder: the
// torrent's name, then the file's path; a torrent without a name has no folder of its own. It is
// passed to the engine and used by Layout, so the two cannot differ.
func filePath(opts storage.FilePathMakerOpts) string {
	var parts []string
	if opts.Info.BestName() != metainfo.NoName {
		parts = append(parts, opts.Info.BestName())
	}
	return filepath.Join(append(parts, opts.File.BestPath()...)...)
}

// Layout is where the engine puts the torrent below saveRoot: contentRoot is the file of a
// single-file v1 torrent and the folder of any other, and paths lists the path of every file. A
// path that would leave saveRoot is an error, as it is for the engine, and so is a torrent whose name
// makes its folder the save folder itself ("", "." or "a/.."): a later deletion of "the torrent's
// folder" would remove the user's whole folder.
func Layout(saveRoot string, info *metainfo.Info) (contentRoot string, paths []string, err error) {
	contentRoot = filepath.Join(saveRoot, info.BestName())
	if info.BestName() == metainfo.NoName {
		contentRoot = saveRoot
	} else if contentRoot == saveRoot {
		return "", nil, fmt.Errorf("the torrent's name %q makes its folder the save folder", info.BestName())
	}
	for _, f := range info.UpvertedFiles() {
		path := filepath.Join(saveRoot, filePath(storage.FilePathMakerOpts{Info: info, File: &f}))
		if rel, relErr := filepath.Rel(saveRoot, path); relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", nil, fmt.Errorf("the file %q would be written outside the save folder %q", path, saveRoot)
		}
		paths = append(paths, path)
	}
	return contentRoot, paths, nil
}

// Ranges is Layout with the pieces every file covers: what files.Decide needs as the torrent's own
// description of its files, and what a record is checked against.
func Ranges(saveRoot string, info *metainfo.Info) ([]files.FileRange, error) {
	_, paths, err := Layout(saveRoot, info)
	if err != nil {
		return nil, err
	}
	ranges := make([]files.FileRange, len(paths))
	for i, f := range info.UpvertedFiles() {
		ranges[i] = files.FileRange{Path: paths[i], First: f.BeginPieceIndex(info.PieceLength), End: f.EndPieceIndex(info.PieceLength)}
	}
	return ranges, nil
}

// refusing is the default storage of the engine client.
type refusing struct{}

// Refusing is the engine client's default storage: it opens no torrent. Every torrent is added with
// storage of its own, made for the folder the user chose.
func Refusing() storage.ClientImpl {
	return refusing{}
}

func (refusing) OpenTorrent(context.Context, *metainfo.Info, metainfo.Hash) (storage.TorrentImpl, error) {
	return storage.TorrentImpl{}, ErrNoSaveFolder
}
