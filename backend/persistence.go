// persistence.go
//
// Loading and saving flow data on disk.
//
// Every saved macro is one JSON file in the app data directory
// (utils.GetAppDataDir). The bare filename, without its extension, doubles as
// the macro's id: it is what ListProjects hands out and what LoadProject
// accepts back.

package backend

import (
	"Keypress/backend/utils"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	// projectFileExt is the on-disk extension of every saved macro.
	projectFileExt = ".json"

	// maxProjectIDLen bounds ids so that no name - however long or hostile -
	// can produce a path the filesystem rejects.
	maxProjectIDLen = 128

	// maxMacroNameLen bounds the display name, which is stored in the file
	// rather than used as a filename and so is not covered by maxProjectIDLen.
	// Without it a paste accident becomes a megabyte of JSON that every
	// ListProjects call then reads back. Counted in runes, because that is what
	// the user typed; the limit is above maxProjectIDLen so that any name short
	// enough to slugify in full is accepted.
	maxMacroNameLen = 200

	// tempFilePattern names the scratch file a save is written to before it is
	// renamed into place. The ".tmp" ending is what keeps one left behind by a
	// crash out of ListProjects, which matches on a ".json" extension.
	tempFilePattern = "macro-*" + projectFileExt + ".tmp"
)

// SaveFile writes the flow data to one JSON file per macro in the app data
// directory, records it as the last opened file, and returns the macro's id.
//
// Despite what this function used to claim, it does not show a native Save File
// dialog. That is deliberate rather than missing: the app owns its data
// directory, and letting the user steer a macro anywhere on disk with
// runtime.SaveFileDialog would put it outside the directory ListProjects
// enumerates. The user picks a name; the app picks the location.
//
// name is the display name the user typed, slugified into the macro's id (its
// bare filename). currentID is the id of the macro the caller already has open,
// empty when this graph has never been saved. Names are unique:
//
//   - an empty or whitespace-only name is rejected. There is no longer a
//     default_flow fallback - a macro saved by accident under a name nobody
//     chose is worse than a save the user has to name;
//   - a name whose id already exists on disk is rejected, UNLESS that id is
//     currentID. Saving the macro you have open back over its own file is the
//     ordinary update path and uniqueness must not stand in its way, or macros
//     could be created but never edited.
//
// Two different display names can slugify to the same id ("My Macro" and
// "my  macro" both give "my-macro"), and the check is on the id, so the second
// one collides rather than quietly overwriting the first.
//
// Giving the open macro a new name is a rename, not a copy: the file it used to
// live in is removed once the new one is safely on disk, so the macro list shows
// it once under its new name instead of twice under both.
//
// A save that fails costs the user nothing that was already saved: the graph is
// written to a temporary file and renamed into place, so the macro on disk is
// either the old one or the new one at every instant. See writeMacroFile.
func (a *App) SaveFile(flowData FlowData, name string, currentID string) (string, error) {
	displayName := strings.TrimSpace(name)

	id, staleFile, err := saveMacro(flowData, displayName, currentID)
	if err != nil {
		log.Printf("SaveFile %q: %v", name, err)
		// Deliberately no "save-error" event: the bound call already rejects
		// with this error, and the frontend surfaces it from there. Emitting
		// as well would report every failure twice.
		return "", err
	}

	// A rename whose old file survived is reported on the success event rather
	// than as a failure, because the save itself is done: the graph is on disk
	// under its new name and that is the macro the workspace now has open. The
	// only consequence is a leftover the user will see in the macro list, so
	// the message names it - being told about a file you can then delete
	// yourself is better than wondering why the old name is still there.
	message := fmt.Sprintf("Saved macro %q", displayName)
	if staleFile != "" {
		message = fmt.Sprintf("%s - its old file %q could not be removed and is still listed", message, staleFile)
	}

	a.emitEvent("save-success", message)
	return id, nil
}

// saveMacro is the whole of SaveFile bar the logging and the event, split out
// so that every failure path returns through one place.
//
// It returns the saved macro's id and, when this save renamed a macro whose old
// file could not be deleted afterwards, the bare filename left behind. That
// leftover is not an error - see SaveFile for why.
//
// displayName is expected already trimmed.
func saveMacro(flowData FlowData, displayName string, currentID string) (id string, staleFile string, err error) {
	if displayName == "" {
		return "", "", errors.New("a macro name is required")
	}
	if length := len([]rune(displayName)); length > maxMacroNameLen {
		return "", "", fmt.Errorf("macro name is too long (%d characters, maximum %d)", length, maxMacroNameLen)
	}

	id = slugifyProjectID(displayName)
	if id == "" {
		// The name was entirely characters the slug drops (punctuation, or a
		// script this ASCII slugifier cannot represent), so there is no
		// filename to build. Say so rather than minting an opaque id the user
		// would never recognise in the macro list.
		return "", "", fmt.Errorf("macro name %q has no letters or digits to build a filename from", displayName)
	}

	fullPath, id, err := projectPath(id)
	if err != nil {
		return "", "", err
	}

	// The open macro's id also arrives from the frontend, and it decides
	// whether an existing file may be overwritten - and now also which file
	// gets DELETED on a rename - so it gets the same filename validation as any
	// other id. Nothing downstream builds a path from the raw currentID.
	openID := ""
	if strings.TrimSpace(currentID) != "" {
		openID, err = sanitizeProjectID(currentID)
		if err != nil {
			return "", "", fmt.Errorf("invalid open macro id: %w", err)
		}
	}
	isOwnFile := openID != "" && openID == id

	// The id is the filename; a copy inside the file would only give the two a
	// way to disagree after a rename.
	flowData.ID = ""
	flowData.Name = displayName

	// A nil slice marshals to JSON null, and ListProjects treats a file with no
	// "nodes" array as something other than a macro and skips it - so a graph
	// the user had emptied would save without complaint and then be missing
	// from the macro list. Empty slices keep an empty macro a macro.
	if flowData.Nodes == nil {
		flowData.Nodes = []Node{}
	}
	if flowData.Edges == nil {
		flowData.Edges = []Edge{}
	}

	jsonData, err := json.MarshalIndent(flowData, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("failed to encode macro %q: %w", displayName, err)
	}
	jsonData = append(jsonData, '\n')

	if err := writeMacroFile(fullPath, jsonData, isOwnFile); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return "", "", fmt.Errorf("a macro named %q already exists (%s%s) - pick a different name", displayName, id, projectFileExt)
		}
		return "", "", fmt.Errorf("failed to save macro %q: %w", displayName, err)
	}

	// Everything past this point is bookkeeping around a macro that is already
	// safely on disk, and none of it may turn a save that worked into a save
	// the user is told failed.

	// The id, not the path: the settings file must survive the data directory
	// moving, and nothing that reads it should ever be handed a path it did not
	// build itself.
	if err := utils.SetLastOpenedProject(id); err != nil {
		// The macro is safely on disk. Forgetting which one was open is a far
		// smaller problem than telling the user the save failed when it did not.
		log.Printf("saveMacro: failed to record %q as last opened: %v", id, err)
	}

	// The last-opened pointer is moved BEFORE the old file is removed so that a
	// failed removal cannot leave the workspace reopening the macro under its
	// old name; the worst case is a leftover file, never a lost or stale graph.
	if openID != "" && !isOwnFile {
		staleFile = removeRenamedFile(openID, fullPath)
	}

	return id, staleFile, nil
}

// removeRenamedFile deletes the file a macro used to live in after it has been
// re-saved under a new id.
//
// It is called only once the new file has been written AND closed, so the graph
// exists in two places for the moment this runs and in one place afterwards -
// never in none. openID has already been through sanitizeProjectID; projectPath
// puts it through again, because this is the one place an id from the frontend
// decides what gets deleted and a second pass costs nothing.
//
// newPath is the file just written, and is compared against purely as a
// backstop: the caller has established that the ids differ, and ids map to
// paths one-to-one, so this can only fire if that ever stops being true.
//
// Failure is reported by returning the bare filename left behind rather than an
// error, because there is nothing here the caller should treat as a failed save.
func removeRenamedFile(openID string, newPath string) string {
	oldPath, oldID, err := projectPath(openID)
	if err != nil {
		log.Printf("removeRenamedFile: refusing to remove the old file for %q: %v", openID, err)
		return ""
	}
	if oldPath == newPath {
		log.Printf("removeRenamedFile: old and new macro resolve to the same file %q; nothing removed", oldPath)
		return ""
	}

	// The rename is only genuine if the old id really was a macro file. A
	// currentID pointing at nothing (a macro deleted from under the app, say)
	// means there is simply nothing to tidy up.
	info, err := os.Stat(oldPath)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("removeRenamedFile: cannot inspect %q: %v", oldPath, err)
		}
		return ""
	}
	if !info.Mode().IsRegular() {
		log.Printf("removeRenamedFile: %q is not a regular file; leaving it alone", oldPath)
		return ""
	}

	if err := os.Remove(oldPath); err != nil {
		log.Printf("removeRenamedFile: failed to remove %q after rename: %v", oldPath, err)
		return oldID + projectFileExt
	}

	return ""
}

// writeMacroFile puts data at fullPath without ever leaving a half-written
// macro there.
//
// The bytes go to a temporary file in the same directory, are flushed to disk,
// and are then renamed over the destination. The rename is atomic and the
// directory is the same one, so it stays on a single filesystem: a crash, a
// power cut or a full disk part-way through leaves either the previous macro or
// the new one, never a truncated file that no longer parses. Writing in place
// would put the user's saved work at the mercy of every write after the first.
//
// replacing says the destination is the caller's own macro and may be
// overwritten. When it is false the name must not already be taken, and that is
// settled by creating the destination empty with O_EXCL *before* the temporary
// file is written: the check and the claim are then one syscall, so a macro
// that appears in between cannot be clobbered by a rename that just found the
// name free. The reservation is removed again if anything after it fails, so a
// failed save never leaves an empty macro behind to block the retry.
//
// A caller that gets fs.ErrExist back is being told the name is taken; every
// other error is a genuine write failure.
func writeMacroFile(fullPath string, data []byte, replacing bool) error {
	if !replacing {
		reservation, err := os.OpenFile(fullPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			// fs.ErrExist included: the caller turns it into the "pick a
			// different name" message, and wrapping it here would only make
			// that harder to spot.
			return err
		}
		if err := reservation.Close(); err != nil {
			removeIgnoringMissing(fullPath)
			return err
		}
	}

	// Same directory as the destination, so the rename below is a rename and
	// not a copy across filesystems.
	tmp, err := os.CreateTemp(filepath.Dir(fullPath), tempFilePattern)
	if err != nil {
		if !replacing {
			removeIgnoringMissing(fullPath)
		}
		return err
	}
	tmpPath := tmp.Name()

	// CreateTemp makes the file 0600, and it is the file that ends up being the
	// macro. Without this every saved macro would quietly become user-only on
	// the platforms where the mode means anything, purely as a side effect of
	// how it was written. Not fatal if it fails: the macro is the point, its
	// group bit is not.
	if err := tmp.Chmod(0644); err != nil {
		log.Printf("persistence: could not set the mode on %q: %v", tmpPath, err)
	}

	if err := writeSyncClose(tmp, data); err != nil {
		removeIgnoringMissing(tmpPath)
		if !replacing {
			removeIgnoringMissing(fullPath)
		}
		return err
	}

	if err := os.Rename(tmpPath, fullPath); err != nil {
		removeIgnoringMissing(tmpPath)
		if !replacing {
			removeIgnoringMissing(fullPath)
		}
		return err
	}

	return nil
}

// writeSyncClose writes data to file, flushes it to disk and closes it,
// reporting whichever step failed.
//
// The Sync is what makes the rename in writeMacroFile worth doing: renaming a
// file whose contents are still only in the page cache would leave the macro
// exactly as losable as writing it in place. Close matters too - on some
// filesystems it is where a failed write finally surfaces.
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

// removeIgnoringMissing deletes path, logging anything except it already being
// gone. Every caller is cleaning up after a failure it is about to report, so
// there is nothing here worth a second error.
func removeIgnoringMissing(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Printf("persistence: failed to clean up %q: %v", path, err)
	}
}

// ListProjects enumerates the saved macros in the app data directory.
//
// A file that is not readable, is not valid JSON, or is JSON but not a flow is
// skipped and logged: one stray file in the data directory must not take the
// whole listing down with it.
func (a *App) ListProjects() ([]ProjectSummary, error) {
	dataDir, err := utils.GetAppDataDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get data directory: %w", err)
	}

	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read data directory: %w", err)
	}

	// Built with make, never nil: a nil slice marshals to JSON null and the
	// frontend expects an array.
	summaries := make([]ProjectSummary, 0, len(entries))

	for _, entry := range entries {
		filename := entry.Name()
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(filename), projectFileExt) {
			continue
		}

		fullPath, id, err := projectPath(filename)
		if err != nil {
			log.Printf("ListProjects: skipping %q: %v", filename, err)
			continue
		}

		data, err := os.ReadFile(fullPath)
		if err != nil {
			log.Printf("ListProjects: skipping %q: %v", filename, err)
			continue
		}

		var flowData FlowData
		if err := json.Unmarshal(data, &flowData); err != nil {
			log.Printf("ListProjects: skipping %q: not valid JSON: %v", filename, err)
			continue
		}
		if flowData.Nodes == nil {
			// Parses, but has no "nodes" key at all - some other JSON file
			// that happens to live in the data directory.
			log.Printf("ListProjects: skipping %q: no nodes, not a flow file", filename)
			continue
		}

		modifiedAt := ""
		if info, err := entry.Info(); err == nil {
			modifiedAt = info.ModTime().UTC().Format(time.RFC3339)
		} else {
			log.Printf("ListProjects: no modification time for %q: %v", filename, err)
		}

		summaries = append(summaries, ProjectSummary{
			ID:         id,
			Name:       projectDisplayName(flowData.Name, id),
			NodeCount:  len(flowData.Nodes),
			EdgeCount:  len(flowData.Edges),
			ModifiedAt: modifiedAt,
		})
	}

	// Most recently touched first, falling back to the id so the order is
	// stable when two files share a timestamp (or have none).
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].ModifiedAt != summaries[j].ModifiedAt {
			return summaries[i].ModifiedAt > summaries[j].ModifiedAt
		}
		return summaries[i].ID < summaries[j].ID
	})

	return summaries, nil
}

// LoadProject loads a single saved macro by the id ListProjects handed out.
//
// The id arrives from the frontend and is turned into a path, so it is
// validated as a bare filename first - see sanitizeProjectID.
func (a *App) LoadProject(id string) (*FlowData, error) {
	fullPath, cleanID, err := projectPath(id)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read project %q: %w", cleanID, err)
	}

	var flowData FlowData
	if err := json.Unmarshal(data, &flowData); err != nil {
		return nil, fmt.Errorf("failed to parse project %q: %w", cleanID, err)
	}

	flowData.ID = cleanID
	flowData.Name = projectDisplayName(flowData.Name, cleanID)
	return &flowData, nil
}

// LoadLastFile loads the macro the workspace had open when it was last used,
// returning nil without an error when there is none to load.
//
// The macro is recorded in the settings file as an id, not a path, so this is
// LoadProject by another name: the id goes through the same projectPath
// validation, which is what keeps a hand-edited config.json from naming a file
// outside the data directory. An id that fails it is treated as no last macro
// at all - the app opens on an empty canvas rather than refusing to start over
// a setting.
//
// "Nothing to load" also covers the macro having been deleted since. The
// remaining errors are a file that is there but unreadable or not a flow, which
// the user should be told about rather than have silently swallowed.
func (a *App) LoadLastFile() (*FlowData, error) {
	id := utils.LoadConfig().LastOpenedProject
	if strings.TrimSpace(id) == "" {
		return nil, nil
	}

	fullPath, cleanID, err := projectPath(id)
	if err != nil {
		log.Printf("LoadLastFile: ignoring the recorded last opened macro: %v", err)
		return nil, nil
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Deleted from the macro list, or from the data directory by hand.
			log.Printf("LoadLastFile: last opened macro %q no longer exists", cleanID)
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read last opened macro %q: %w", cleanID, err)
	}

	var flowData FlowData
	if err := json.Unmarshal(data, &flowData); err != nil {
		return nil, fmt.Errorf("failed to parse last opened macro %q: %w", cleanID, err)
	}

	// The id is the filename, so it is not in the file - but the caller needs it
	// to save this macro back over its own file rather than be told the name is
	// already taken.
	flowData.ID = cleanID
	flowData.Name = projectDisplayName(flowData.Name, cleanID)
	return &flowData, nil
}

// projectPath resolves a macro id to its absolute path inside the app data
// directory, returning the validated bare id alongside it. The id may be given
// with or without the .json extension.
func projectPath(id string) (fullPath string, cleanID string, err error) {
	cleanID, err = sanitizeProjectID(id)
	if err != nil {
		return "", "", err
	}

	dataDir, err := utils.GetAppDataDir()
	if err != nil {
		return "", "", fmt.Errorf("failed to get data directory: %w", err)
	}

	dataDir = filepath.Clean(dataDir)
	fullPath = filepath.Clean(filepath.Join(dataDir, cleanID+projectFileExt))

	// Belt and braces. sanitizeProjectID has already rejected everything that
	// could traverse, but the value came from the frontend, so confirm the
	// joined path really did land directly in the data directory.
	if filepath.Dir(fullPath) != dataDir {
		return "", "", fmt.Errorf("invalid project id %q: escapes the data directory", id)
	}

	return fullPath, cleanID, nil
}

// sanitizeProjectID validates an id that arrived from the frontend and returns
// the bare filename (no extension) it maps to.
//
// The id is treated strictly as a filename: anything that could reach outside
// the app data directory - path separators, volume letters, "..", a leading dot
// - is rejected outright rather than cleaned up, because a "repaired" id would
// silently address a different file than the caller asked for.
func sanitizeProjectID(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("project id is empty")
	}

	if strings.EqualFold(filepath.Ext(id), projectFileExt) {
		id = id[:len(id)-len(projectFileExt)]
	}
	if id == "" {
		return "", fmt.Errorf("project id is empty")
	}

	if len(id) > maxProjectIDLen {
		return "", fmt.Errorf("project id is too long (%d > %d)", len(id), maxProjectIDLen)
	}
	if strings.Contains(id, "..") {
		return "", fmt.Errorf("invalid project id %q: contains %q", id, "..")
	}
	if strings.HasPrefix(id, ".") {
		return "", fmt.Errorf("invalid project id %q: starts with a dot", id)
	}
	if strings.ContainsAny(id, `/\:`) || id != filepath.Base(id) || filepath.IsAbs(id) {
		return "", fmt.Errorf("invalid project id %q: must be a bare filename", id)
	}

	for _, r := range id {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		if r == '-' || r == '_' || r == '.' || r == ' ' {
			continue
		}
		return "", fmt.Errorf("invalid project id %q: unsupported character %q", id, r)
	}

	return id, nil
}

// slugifyProjectID turns a user-supplied macro name into a safe bare filename.
// Everything outside [a-z0-9_-] collapses to a single dash. The result can be
// empty (for a name made entirely of dropped characters); callers decide what
// to do about that.
func slugifyProjectID(name string) string {
	var b strings.Builder
	pendingDash := false

	for _, r := range strings.ToLower(name) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			if pendingDash && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingDash = false
			b.WriteRune(r)
		case r == '-' || r == '_':
			if b.Len() > 0 {
				pendingDash = false
				b.WriteRune(r)
			}
		default:
			pendingDash = true
		}
	}

	slug := strings.Trim(b.String(), "-_")
	if len(slug) > maxProjectIDLen {
		slug = strings.Trim(slug[:maxProjectIDLen], "-_")
	}

	return slug
}

// projectDisplayName returns the stored name of a macro, falling back to one
// derived from its id for files saved before macros carried a name.
func projectDisplayName(stored string, id string) string {
	if name := strings.TrimSpace(stored); name != "" {
		return name
	}

	words := strings.FieldsFunc(id, func(r rune) bool {
		return r == '-' || r == '_' || r == ' '
	})
	if len(words) == 0 {
		return id
	}

	for i, word := range words {
		runes := []rune(word)
		runes[0] = unicode.ToUpper(runes[0])
		words[i] = string(runes)
	}

	return strings.Join(words, " ")
}
