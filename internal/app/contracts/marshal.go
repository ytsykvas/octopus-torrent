package contracts

import (
	"encoding/json"
	"time"
)

// The outgoing DTOs normalize themselves when encoded, so a service cannot break the contract by
// building a value with a nil list or a local time: lists are never null on the wire (the frontend
// rejects null where a list is required) and times are always UTC.

func (s LibrarySnapshot) MarshalJSON() ([]byte, error) {
	type fields LibrarySnapshot
	s.Torrents = nonNil(s.Torrents)
	return json.Marshal(fields(s))
}

func (d LibraryDelta) MarshalJSON() ([]byte, error) {
	type fields LibraryDelta
	d.Upserted, d.Removed = nonNil(d.Upserted), nonNil(d.Removed)
	return json.Marshal(fields(d))
}

func (t TorrentSnapshot) MarshalJSON() ([]byte, error) {
	type fields TorrentSnapshot
	t.Files, t.Errors, t.AddedAt = nonNil(t.Files), nonNil(t.Errors), encodableTime(t.AddedAt)
	return json.Marshal(fields(t))
}

// encodableTime is t in UTC. A year outside RFC 3339's 0–9999 (a corrupt stored value) would make
// the snapshot unencodable, and Wails exits on a result it cannot encode, so it becomes the zero
// time instead: the record stays visible and can be removed.
func encodableTime(t time.Time) time.Time {
	if t = t.UTC(); t.Year() < 0 || t.Year() > 9999 {
		return time.Time{}
	}
	return t
}

func (p ImportPreview) MarshalJSON() ([]byte, error) {
	type fields ImportPreview
	p.Files = nonNil(p.Files)
	return json.Marshal(fields(p))
}

func (e AppError) MarshalJSON() ([]byte, error) {
	type fields AppError
	if e.Params == nil {
		e.Params = map[string]string{}
	}
	return json.Marshal(fields(e))
}

func nonNil[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}
