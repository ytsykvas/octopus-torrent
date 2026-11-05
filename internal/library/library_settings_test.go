package library

import (
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"octopustorrent/internal/app/contracts"
)

// settingsOf reads the stored settings, failing the test on an error.
func settingsOf(t *testing.T, lib *Library) (contracts.Settings, bool) {
	t.Helper()
	s, found, err := lib.Settings(deadline(t, 5*time.Second))
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	return s, found
}

func TestNoSettingsAreStoredUntilTheFirstSave(t *testing.T) {
	lib, _ := openLibrary(t)
	if s, found := settingsOf(t, lib); found || !reflect.DeepEqual(s, contracts.Settings{}) {
		t.Fatalf("Settings of a new library = %+v, %v; want none", s, found)
	}
}

func TestSavedSettingsAreReadBackExactlyAfterAReopen(t *testing.T) {
	lib, path := openLibrary(t)
	folder := "/Volumes/Données/Torrents ☂"
	most, least := contracts.BytesPerSecond(math.MaxUint64), contracts.BytesPerSecond(1)
	every := contracts.Settings{
		DefaultSavePath: &folder, Theme: contracts.ThemeDark, Language: "uk",
		DownloadLimit: &most, UploadLimit: &least, GlobalUploadDisabled: true,
	}
	// A field added to the settings without a column would be lost silently: every exported one is
	// set here.
	for i := range reflect.TypeOf(every).NumField() {
		if field := reflect.TypeOf(every).Field(i); field.IsExported() && reflect.ValueOf(every).Field(i).IsZero() {
			t.Fatalf("the round trip leaves Settings.%s unset; store it and set it here", field.Name)
		}
	}
	none := contracts.Settings{Theme: contracts.ThemeSystem, Language: "en"}
	for _, want := range []contracts.Settings{every, none, every} {
		if err := lib.SaveSettings(deadline(t, 5*time.Second), want); err != nil {
			t.Fatalf("SaveSettings(%+v): %v", want, err)
		}
		lib = reopen(t, lib, path)
		if got, found := settingsOf(t, lib); !found || !reflect.DeepEqual(got, want) {
			t.Fatalf("Settings after a save and a reopen = %+v, %v; want %+v", got, found, want)
		}
	}
	if n := rowCounts(t, lib.store)["settings"]; n != 1 {
		t.Fatalf("%d settings rows, want 1", n)
	}
}

func TestInvalidSettingsAreRefusedAndKeepTheStoredOnes(t *testing.T) {
	lib, _ := openLibrary(t)
	stored := contracts.Settings{Theme: contracts.ThemeLight, Language: "de"}
	if err := lib.SaveSettings(deadline(t, 5*time.Second), stored); err != nil {
		t.Fatal(err)
	}
	relative, zero := "Downloads", contracts.BytesPerSecond(0)
	for name, bad := range map[string]contracts.Settings{
		"a relative folder": {DefaultSavePath: &relative, Theme: contracts.ThemeDark, Language: "en"},
		"an unknown theme":  {Theme: "sepia", Language: "en"},
		"no language":       {Theme: contracts.ThemeDark},
		"a zero limit":      {Theme: contracts.ThemeDark, Language: "en", UploadLimit: &zero},
	} {
		if err := lib.SaveSettings(deadline(t, 5*time.Second), bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("SaveSettings with %s = %v, want ErrInvalid", name, err)
		}
	}
	if got, _ := settingsOf(t, lib); !reflect.DeepEqual(got, stored) {
		t.Fatalf("Settings after refused saves = %+v, want %+v", got, stored)
	}
}
