package filestore

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	g "github.com/anacrolix/generics"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

// ErrNoSaveFolder is returned by the default storage of the engine client: a torrent added without
// storage of its own has no folder the user chose, and nothing is written anywhere.
var ErrNoSaveFolder = errors.New("the torrent has no save folder: it needs storage of its own")

// NewTorrentStorage is the storage for one torrent: the engine's file storage with
// the payload under saveRoot, part files off, and the given completion, inside a guard. A nil logger
// is slog.Default.
func NewTorrentStorage(saveRoot string, completion storage.PieceCompletion, session *Session, logger *slog.Logger) (*TorrentStorage, error) {
	inner := storage.NewFileOpts(storage.NewFileClientOpts{
		ClientBaseDir:   saveRoot,
		FilePathMaker:   filePath,
		PieceCompletion: completion,
		UsePartFiles:    g.Some(false),
		Logger:          logger,
	})
	return newTorrentStorage(inner, saveRoot, session)
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
// single-file v1 torrent and the folder of any other, and files lists the path of every file. A
// path that would leave saveRoot is an error, as it is for the engine.
func Layout(saveRoot string, info *metainfo.Info) (contentRoot string, files []string, err error) {
	contentRoot = filepath.Join(saveRoot, info.BestName())
	if info.BestName() == metainfo.NoName {
		contentRoot = saveRoot
	}
	for _, f := range info.UpvertedFiles() {
		path := filepath.Join(saveRoot, filePath(storage.FilePathMakerOpts{Info: info, File: &f}))
		if rel, relErr := filepath.Rel(saveRoot, path); relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", nil, fmt.Errorf("the file %q would be written outside the save folder %q", path, saveRoot)
		}
		files = append(files, path)
	}
	return contentRoot, files, nil
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
