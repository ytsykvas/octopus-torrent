package library

import (
	"context"
	"database/sql"
	"errors"

	"octopustorrent/internal/app/contracts"
)

// Settings returns the owner's saved settings. found is false until the first save: the caller
// then uses its defaults (the first language comes from macOS, SET-06; upload is allowed).
func (l *Library) Settings(ctx context.Context) (saved contracts.Settings, found bool, err error) {
	err = l.store.Read(ctx, func(tx txn) error {
		var path sql.NullString
		var download, upload sql.NullInt64
		err := tx.QueryRow(`SELECT default_save_path, theme, language, download_limit, upload_limit, global_upload_disabled
			FROM settings WHERE id = 1`).Scan(&path, &saved.Theme, &saved.Language, &download, &upload,
			&saved.GlobalUploadDisabled)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		saved.DefaultSavePath = nullString(path)
		saved.DownloadLimit, saved.UploadLimit = rate(download), rate(upload)
		return nil
	})
	if err != nil {
		return contracts.Settings{}, false, err
	}
	return saved, found, nil
}

// SaveSettings stores all settings at once (the first save creates the row), the global upload stop
// and the limits included.
func (l *Library) SaveSettings(ctx context.Context, s contracts.Settings) error {
	if problem := s.Validate(); problem != nil {
		return invalid("settings: %s %s", problem.Params["field"], problem.Params["reason"])
	}
	return l.store.Write(ctx, func(tx txn) error {
		return execAll(tx, []statement{{
			`INSERT INTO settings(id, default_save_path, theme, language, download_limit, upload_limit, global_upload_disabled)
				VALUES (1, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(id) DO UPDATE SET default_save_path = excluded.default_save_path, theme = excluded.theme,
					language = excluded.language, download_limit = excluded.download_limit,
					upload_limit = excluded.upload_limit, global_upload_disabled = excluded.global_upload_disabled`,
			[]any{s.DefaultSavePath, s.Theme, s.Language, rateArg(s.DownloadLimit), rateArg(s.UploadLimit), s.GlobalUploadDisabled},
		}})
	})
}

// rateArg is a limit as its int64 bit pattern (database/sql refuses a uint64 with the high bit set),
// or NULL for none.
func rateArg(limit *contracts.BytesPerSecond) any {
	if limit == nil {
		return nil
	}
	return int64(*limit)
}

// rate is a stored limit, nil for none.
func rate(stored sql.NullInt64) *contracts.BytesPerSecond {
	if !stored.Valid {
		return nil
	}
	limit := contracts.BytesPerSecond(stored.Int64)
	return &limit
}

// nullString is a stored text, nil for NULL.
func nullString(stored sql.NullString) *string {
	if !stored.Valid {
		return nil
	}
	return &stored.String
}
