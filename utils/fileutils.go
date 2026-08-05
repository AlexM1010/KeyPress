package utils

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/adrg/xdg"
)

const (
	AppName = "Keypress"

	// configFileName is the app's settings file, in the app config directory.
	// One structured file, so the next preference (window size, theme, a recent
	// list) has an obvious home instead of another single-purpose file beside
	// it.
	configFileName = "config.json"

	// legacyLastOpenedFileName is the file config.json replaced: one bare
	// absolute filesystem path in plain text. Nothing writes it any more; the
	// name survives only so LoadConfig can carry that last scrap of state into
	// config.json once and then delete the file.
	legacyLastOpenedFileName = "last_opened_file.txt"
)

// Config is the app's persisted settings.
//
// It is deliberately a struct in one JSON file rather than a file per setting:
// adding a preference is a field here, and readers of an older file get the
// zero value for anything they do not find.
type Config struct {
	// LastOpenedProject is the macro to reopen at startup, held as its id - the
	// bare filename its data file has in the app data directory - and never as
	// a path.
	//
	// An id survives the data directory or the user profile moving, is portable
	// between machines, and cannot address a file outside that directory: the
	// reader resolves it to a path through the same validation any id from the
	// frontend gets. Empty means there is no last opened macro.
	LastOpenedProject string `json:"lastOpenedProject,omitempty"`
}

// GetAppConfigDir returns the application-specific config directory
func GetAppConfigDir() (string, error) {
	configPath := filepath.Join(AppName, "configs")
	path, err := xdg.ConfigFile(configPath)
	if err != nil {
		return "", fmt.Errorf("failed to get config directory: %w", err)
	}
	// Ensure the directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}
	return dir, nil
}

// GetAppDataDir returns the application-specific data directory
func GetAppDataDir() (string, error) {
	dataPath := filepath.Join(AppName, "data")
	path, err := xdg.DataFile(dataPath)
	if err != nil {
		return "", fmt.Errorf("failed to get data directory: %w", err)
	}
	// Ensure the directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create data directory: %w", err)
	}
	return dir, nil
}

// LoadConfig reads the settings file.
//
// It never fails. Missing, empty, unreadable or not-JSON all give the zero
// Config - no last opened macro, every future setting at its default - and are
// logged rather than returned, because the file is hand-editable and settings
// are a convenience: none of them is worth refusing to start the app over.
// Callers therefore treat what comes back as untrusted input and validate it.
//
// It is also where the plain-text last_opened_file.txt this file replaced is
// picked up and removed, once, on the first run that finds no config.json.
func LoadConfig() Config {
	path, err := configFilePath()
	if err != nil {
		log.Printf("config: continuing without settings: %v", err)
		return Config{}
	}

	cfg, ok := readConfig(path)
	if !ok {
		return adoptLegacyLastOpened(path)
	}

	// config.json is what the app reads now, so the file it superseded is
	// nothing but a stale copy of one setting - drop it. Normally already gone;
	// this catches the run where the removal below failed.
	removeLegacyLastOpened(legacyLastOpenedPath(path))

	return cfg
}

// SetLastOpenedProject records id as the macro to reopen at startup and leaves
// every other setting in the file as it was.
//
// id is a macro id, never a path; "" forgets the last opened macro. A settings
// file that cannot be parsed is rebuilt from scratch rather than turned into a
// failed write - the one setting in hand is worth more than an unreadable file.
func SetLastOpenedProject(id string) error {
	path, err := configFilePath()
	if err != nil {
		return err
	}

	cfg, _ := readConfig(path)
	cfg.LastOpenedProject = id

	return writeConfig(path, cfg)
}

// configFilePath returns the absolute path of the settings file, creating the
// config directory if it is not there yet.
func configFilePath() (string, error) {
	dir, err := GetAppConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configFileName), nil
}

// readConfig parses the settings file, reporting false for every reason there
// is nothing usable to read - which the callers treat as "no settings yet".
func readConfig(path string) (Config, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("config: cannot read %q, ignoring it: %v", path, err)
		}
		return Config{}, false
	}

	if strings.TrimSpace(string(data)) == "" {
		return Config{}, false
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Printf("config: %q is not valid JSON, ignoring it: %v", path, err)
		return Config{}, false
	}

	return cfg, true
}

// writeConfig replaces the settings file with cfg.
//
// The new contents are written to a temporary file in the same directory and
// renamed over the old one, so a crash or a full disk mid-write leaves the
// previous settings intact instead of a truncated file that reads as garbage.
// The rename is atomic, and same-directory keeps it on one filesystem.
func writeConfig(path string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode settings: %w", err)
	}
	data = append(data, '\n')

	// The ".tmp" extension keeps a temporary file that outlives a crash out of
	// anything that enumerates the directory looking for .json macros.
	tmp, err := os.CreateTemp(filepath.Dir(path), "config-*.json.tmp")
	if err != nil {
		return fmt.Errorf("failed to create a temporary settings file: %w", err)
	}
	tmpPath := tmp.Name()

	if err := writeSyncClose(tmp, data); err != nil {
		removeIgnoringMissing(tmpPath)
		return fmt.Errorf("failed to write settings: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		removeIgnoringMissing(tmpPath)
		return fmt.Errorf("failed to replace %q: %w", path, err)
	}

	return nil
}

// writeSyncClose writes data to file, flushes it to disk and closes it. The
// Sync is what makes the rename in writeConfig meaningful: renaming a file
// whose contents are still only in the page cache would leave the settings just
// as losable as writing them in place.
func writeSyncClose(file *os.File, data []byte) error {
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// adoptLegacyLastOpened carries the macro recorded in last_opened_file.txt into
// config.json and removes that file, so the switch does not cost the user the
// macro they had open.
//
// The recorded absolute path is reduced to an id here - the whole point of the
// new format is that no path is kept - and the id is not validated on the way
// in: it is validated where it is used, like any other id.
//
// The file is removed only once its contents are safely in config.json; if that
// write fails it is left alone for the next run to try again.
func adoptLegacyLastOpened(configPath string) Config {
	legacyPath := legacyLastOpenedPath(configPath)

	data, err := os.ReadFile(legacyPath)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("config: cannot read %q, ignoring it: %v", legacyPath, err)
		}
		return Config{}
	}

	cfg := Config{LastOpenedProject: legacyProjectID(strings.TrimSpace(string(data)))}
	if cfg.LastOpenedProject != "" {
		if err := writeConfig(configPath, cfg); err != nil {
			log.Printf("config: failed to carry the last opened macro into %q: %v", configPath, err)
			return cfg
		}
		log.Printf("config: carried the last opened macro over from %q", legacyPath)
	}

	removeLegacyLastOpened(legacyPath)
	return cfg
}

// legacyProjectID turns a path recorded by the old scheme into a macro id.
//
// It yields "" for anything the id scheme cannot describe - most of all a path
// outside the app data directory, left over from the days of the native save
// dialog - because such a file is not a macro the app can name, and inventing
// an id for it would point the reader at a different file that happens to share
// its basename.
func legacyProjectID(path string) string {
	if path == "" {
		return ""
	}

	dataDir, err := GetAppDataDir()
	if err != nil {
		return ""
	}

	// EqualFold for Windows, where the recorded path and the data directory can
	// differ only in case and still be the same directory.
	if !strings.EqualFold(filepath.Clean(filepath.Dir(path)), filepath.Clean(dataDir)) {
		return ""
	}

	name := filepath.Base(path)
	return strings.TrimSuffix(name, filepath.Ext(name))
}

// legacyLastOpenedPath returns where last_opened_file.txt sits, given the
// settings file that replaced it.
func legacyLastOpenedPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), legacyLastOpenedFileName)
}

// removeLegacyLastOpened deletes the superseded plain-text file. Failing to is
// only clutter, so it is logged and never returned.
func removeLegacyLastOpened(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Printf("config: failed to remove the superseded %q: %v", path, err)
	}
}

// removeIgnoringMissing deletes path, logging anything but its absence.
func removeIgnoringMissing(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Printf("config: failed to clean up %q: %v", path, err)
	}
}
