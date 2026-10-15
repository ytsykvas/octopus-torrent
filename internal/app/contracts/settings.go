package contracts

import "path/filepath"

// Settings are the user's saved preferences (SET-01, SET-04, SET-06, SET-08, TR-04). The frontend
// keeps its own draft and shows these only once Go saved them.
type Settings struct {
	// DefaultSavePath is the absolute folder for future imports; null = not chosen yet, the import
	// asks for one.
	DefaultSavePath *string  `json:"defaultSavePath"`
	Theme           Theme    `json:"theme"`
	Language        Language `json:"language"`
	// DownloadLimit and UploadLimit are in bytes per second; null = unlimited. Zero is not a limit:
	// stopping all upload is GlobalUploadDisabled (TR-04, TR-08).
	DownloadLimit        *BytesPerSecond `json:"downloadLimit" ts_type:"string"`
	UploadLimit          *BytesPerSecond `json:"uploadLimit" ts_type:"string"`
	GlobalUploadDisabled bool            `json:"globalUploadDisabled"`

	shape shape
}

// UnmarshalJSON decodes field by field; see decodeFields.
func (s *Settings) UnmarshalJSON(data []byte) error {
	type fields Settings
	var f fields
	problems, err := decodeFields(data, &f)
	*s = Settings(f)
	s.shape = problems
	return err
}

// validate checks the settings as part of a request; fields are reported under prefix.
func (s *Settings) validate(prefix string) *AppError {
	if p := s.shape.problem(prefix); p != nil {
		return p
	}
	switch {
	case s.DefaultSavePath != nil && !filepath.IsAbs(*s.DefaultSavePath):
		return InvalidRequest(prefix+"defaultSavePath", ReasonFormat)
	case !s.Theme.Valid():
		return InvalidRequest(prefix+"theme", ReasonValue)
	case !s.Language.Valid():
		return InvalidRequest(prefix+"language", ReasonValue)
	case s.DownloadLimit != nil && *s.DownloadLimit == 0:
		return InvalidRequest(prefix+"downloadLimit", ReasonRange)
	case s.UploadLimit != nil && *s.UploadLimit == 0:
		return InvalidRequest(prefix+"uploadLimit", ReasonRange)
	}
	return nil
}
