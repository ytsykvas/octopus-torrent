// Package fixture generates OctopusTorrent's own test torrents: deterministic payloads and v1,
// v2-only and hybrid metainfo, valid and invalid. The generated .torrent files and fixtures.json
// are committed in testdata/; a test compares them with a fresh build.
package fixture

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/anacrolix/torrent/metainfo"
)

// Engine is the module whose behaviour the Engine field of every fixture records. A test fails when
// the build uses another version: the results are then re-recorded.
const Engine = "github.com/anacrolix/torrent v1.61.0"

// Format is the BitTorrent version of a metainfo file.
type Format string

const (
	V1     Format = "v1"
	V2     Format = "v2"
	Hybrid Format = "hybrid"
)

// Stage is the first step of the pinned engine that rejects a metainfo file.
type Stage string

const (
	StageLoad        Stage = "load"        // metainfo.Load: bencode of the file
	StageInfo        Stage = "info"        // MetaInfo.UnmarshalInfo: the info dictionary
	StagePieceLayers Stage = "pieceLayers" // metainfo.ValidatePieceLayers (v2 and hybrid)
	StageAdd         Stage = "add"         // Client.AddTorrent; checked by the swarm tests
	StageAccepted    Stage = "accepted"    // no step rejects it
)

// File is one payload file: its path inside the torrent (for a single-file torrent, its name), its
// length and the SHA-256 of its content.
type File struct {
	Path   []string `json:"path"`
	Length int64    `json:"length"`
	SHA256 string   `json:"sha256"`
}

// Fixture is one generated test torrent. Files is nil for a metainfo-only (invalid) fixture.
type Fixture struct {
	Name        string `json:"name"`
	Purpose     string `json:"purpose"`
	Format      Format `json:"format"`
	PieceLength int64  `json:"pieceLength"`
	Pieces      int    `json:"pieces"`
	InfohashV1  string `json:"infohashV1,omitempty"`
	InfohashV2  string `json:"infohashV2,omitempty"`
	Files       []File `json:"files,omitempty"`
	Engine      Stage  `json:"engine"`
	Metainfo    []byte `json:"-"`

	set, root string
	segments  []segment
}

// MetaInfo parses the fixture's metainfo with the engine.
func (f Fixture) MetaInfo() (*metainfo.MetaInfo, error) {
	return metainfo.Load(bytes.NewReader(f.Metainfo))
}

// ParseStage runs the engine's parsing steps and returns the first that rejects the metainfo with
// its error, or "" when every step accepts it. Adding it to a client is checked by the swarm.
func (f Fixture) ParseStage() (Stage, error) {
	mi, err := f.MetaInfo()
	if err != nil {
		return StageLoad, err
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return StageInfo, err
	}
	if info.HasV2() {
		if err := metainfo.ValidatePieceLayers(mi.PieceLayers, &info.FileTree, info.PieceLength); err != nil {
			return StagePieceLayers, err
		}
	}
	return "", nil
}

var catalog = sync.OnceValue(build)

func build() []Fixture {
	var fixtures []Fixture
	for _, s := range specs() {
		fixtures = append(fixtures, s.build())
	}
	return fixtures
}

// All returns every fixture in catalog order. The fixtures share their slices: do not modify them.
func All() []Fixture {
	return slices.Clone(catalog())
}

// Named returns the fixture with that name.
func Named(name string) (Fixture, error) {
	for _, fx := range catalog() {
		if fx.Name == name {
			return fx, nil
		}
	}
	return Fixture{}, fmt.Errorf("no fixture named %q", name)
}

// WriteCatalog writes every fixture's .torrent file and fixtures.json (the manifest) into dir.
func WriteCatalog(dir string) error {
	for _, fx := range catalog() {
		if err := os.WriteFile(filepath.Join(dir, fx.Name+".torrent"), fx.Metainfo, 0o644); err != nil {
			return err
		}
	}
	manifest := struct {
		Engine   string    `json:"engine"`
		Fixtures []Fixture `json:"fixtures"`
	}{Engine, catalog()}
	data, _ := json.MarshalIndent(manifest, "", "  ") // plain strings, numbers and slices: cannot fail
	return os.WriteFile(filepath.Join(dir, "fixtures.json"), append(data, '\n'), 0o644)
}
