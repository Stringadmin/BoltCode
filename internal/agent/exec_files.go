package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Stringadmin/BoltCode/internal/fio"
)

func (a *Agent) toolReadFile(path, offsetStr, limitStr string) (string, bool) {
	full, err := safeJoin(a.Workdir, path)
	if err != nil {
		return err.Error(), true
	}
	data, err := a.fileSystem().ReadFile(full)
	if err != nil {
		return fmt.Sprintf("error reading %s: %v", path, err), true
	}
	lines := strings.Split(string(data), "\n")

	// Parse offset (1-based line number to start from).
	offset := 0
	if offsetStr != "" {
		if n, err := strconv.Atoi(offsetStr); err == nil && n > 0 {
			offset = n - 1 // convert to 0-based
		}
	}
	if offset >= len(lines) {
		offset = 0
	}

	// Parse limit (max number of lines to read).
	limit := 0
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
	}

	end := len(lines)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}

	// Build output with line numbers so the AI knows which lines it's reading.
	var b strings.Builder
	for i := offset; i < end; i++ {
		fmt.Fprintf(&b, "%5d: %s\n", i+1, lines[i])
	}
	totalLines := len(lines)
	if end < totalLines {
		fmt.Fprintf(&b, "... (%d more lines, use offset=%d to continue)", totalLines-end, end+1)
	}
	return b.String(), false
}

func (a *Agent) toolWriteFile(path, content string) (string, bool) {
	full, err := safeJoin(a.Workdir, path)
	if err != nil {
		return err.Error(), true
	}
	dir := filepath.Dir(full)
	if err := a.fileSystem().MkdirAll(dir, 0o755); err != nil {
		return fmt.Sprintf("error creating dir: %v", err), true
	}
	if err := a.fileSystem().WriteFile(full, []byte(content), 0o644); err != nil {
		return fmt.Sprintf("error writing %s: %v", path, err), true
	}
	return fmt.Sprintf("wrote %s (%d bytes)", path, len(content)), false
}

func (a *Agent) toolEditFile(path, oldStr, newStr string) (string, bool) {
	full, err := safeJoin(a.Workdir, path)
	if err != nil {
		return err.Error(), true
	}
	data, err := a.fileSystem().ReadFile(full)
	if err != nil {
		return fmt.Sprintf("error reading %s: %v", path, err), true
	}
	content := string(data)
	count := strings.Count(content, oldStr)
	switch count {
	case 0:
		return fmt.Sprintf("old_string not found in %s", path), true
	case 1:
		// unambiguous — apply the edit
	default:
		// Multiple matches: silently replacing only the first would risk
		// editing the wrong location without the AI noticing. Surface it so
		// the agent can provide more surrounding context to disambiguate.
		return fmt.Sprintf("old_string matches %d locations in %s — include more surrounding context so it is unique", count, path), true
	}
	updated := strings.Replace(content, oldStr, newStr, 1)
	if err := a.fileSystem().WriteFile(full, []byte(updated), 0o644); err != nil {
		return fmt.Sprintf("error writing %s: %v", path, err), true
	}
	return fmt.Sprintf("edited %s", path), false
}

func (a *Agent) toolDeleteFile(path string) (string, bool) {
	full, err := safeJoin(a.Workdir, path)
	if err != nil {
		return err.Error(), true
	}
	fi, err := a.fileSystem().Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("path not found: %s", path), true
		}
		return fmt.Sprintf("error accessing %s: %v", path, err), true
	}
	if fi.IsDir() {
		// Only allow deleting empty directories.
		entries, err := a.fileSystem().ReadDir(full)
		if err != nil {
			return fmt.Sprintf("error reading directory %s: %v", path, err), true
		}
		if len(entries) > 0 {
			return fmt.Sprintf("cannot delete non-empty directory %s — remove files inside first or use run_command", path), true
		}
		if err := a.fileSystem().Remove(full); err != nil {
			return fmt.Sprintf("error removing directory %s: %v", path, err), true
		}
		return fmt.Sprintf("deleted empty directory %s", path), false
	}
	if err := a.fileSystem().Remove(full); err != nil {
		return fmt.Sprintf("error deleting %s: %v", path, err), true
	}
	return fmt.Sprintf("deleted %s", path), false
}

func (a *Agent) toolRenameFile(from, to string) (string, bool) {
	fullFrom, err := safeJoin(a.Workdir, from)
	if err != nil {
		return err.Error(), true
	}
	fullTo, err := safeJoin(a.Workdir, to)
	if err != nil {
		return err.Error(), true
	}
	// Check source exists.
	if _, err := a.fileSystem().Stat(fullFrom); err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("source not found: %s", from), true
		}
		return fmt.Sprintf("error accessing source %s: %v", from, err), true
	}
	// Create parent directories of destination.
	if err := a.fileSystem().MkdirAll(filepath.Dir(fullTo), 0o755); err != nil {
		return fmt.Sprintf("error creating destination directory: %v", err), true
	}
	if err := a.fileSystem().Rename(fullFrom, fullTo); err != nil {
		// os.Rename can fail across filesystem boundaries on Unix.
		// Fall back to copy + delete.
		return a.copyThenDelete(fullFrom, fullTo, from, to)
	}
	return fmt.Sprintf("renamed %s → %s", from, to), false
}

func (a *Agent) copyThenDelete(src, dst, from, to string) (string, bool) {
	info, err := a.fileSystem().Stat(src)
	if err != nil {
		return fmt.Sprintf("error accessing %s for cross-device move: %v", from, err), true
	}
	if info.IsDir() {
		return fmt.Sprintf("cross-device rename only supports files: %s", from), true
	}
	data, err := a.fileSystem().ReadFile(src)
	if err != nil {
		return fmt.Sprintf("error reading %s for cross-device move: %v", from, err), true
	}
	// Write atomically in the destination filesystem and preserve the source
	// permission bits. Same-device os.Rename already preserves the inode and
	// its mode; only this copy fallback needs to restore them explicitly.
	if err := fio.WriteFileAtomic(dst, data, info.Mode().Perm()); err != nil {
		return fmt.Sprintf("error writing %s during cross-device move: %v", to, err), true
	}
	if err := a.fileSystem().Remove(src); err != nil {
		return fmt.Sprintf("moved content but could not remove %s: %v", from, err), true
	}
	return fmt.Sprintf("renamed %s → %s (cross-device)", from, to), false
}

// toolMultiEdit applies several old→new string replacements to one file in a
// single call. Each old_string must be unique; the first non-matching edit
// aborts the whole operation (no partial writes) so the file stays consistent.
func (a *Agent) toolMultiEdit(toolUseID string, raw json.RawMessage) ToolResult {
	var in struct {
		Path  string `json:"path"`
		Edits []struct {
			Old string `json:"old_string"`
			New string `json:"new_string"`
		} `json:"edits"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return ToolResult{ToolUseID: toolUseID, Content: fmt.Sprintf("malformed multi_edit input: %v", err), IsError: true}
	}
	full, err := safeJoin(a.Workdir, in.Path)
	if err != nil {
		return ToolResult{ToolUseID: toolUseID, Content: err.Error(), IsError: true}
	}
	data, err := a.fileSystem().ReadFile(full)
	if err != nil {
		return ToolResult{ToolUseID: toolUseID, Content: fmt.Sprintf("error reading %s: %v", in.Path, err), IsError: true}
	}
	content := string(data)

	// Validate ALL edits before applying any, so a bad edit doesn't leave a
	// half-modified file.
	for i, e := range in.Edits {
		count := strings.Count(content, e.Old)
		if count == 0 {
			return ToolResult{ToolUseID: toolUseID, Content: fmt.Sprintf("edit %d: old_string not found in %s", i+1, in.Path), IsError: true}
		}
		if count > 1 {
			return ToolResult{ToolUseID: toolUseID, Content: fmt.Sprintf("edit %d: old_string matches %d locations in %s — include more context", i+1, count, in.Path), IsError: true}
		}
	}
	// Apply sequentially.
	for _, e := range in.Edits {
		content = strings.Replace(content, e.Old, e.New, 1)
	}
	if err := a.fileSystem().WriteFile(full, []byte(content), 0o644); err != nil {
		return ToolResult{ToolUseID: toolUseID, Content: fmt.Sprintf("error writing %s: %v", in.Path, err), IsError: true}
	}
	// Combined compact diff for the footer (not sent to the AI), capped so a
	// large batch cannot flood the display.
	var diffParts []string
	for i, e := range in.Edits {
		if i >= 3 {
			diffParts = append(diffParts, fmt.Sprintf(ansiDarkGray+"  │ … %d more edits"+ansiReset, len(in.Edits)-3))
			break
		}
		diffParts = append(diffParts, simpleDiff(e.Old, e.New, 2))
	}
	return ToolResult{ToolUseID: toolUseID, Content: fmt.Sprintf("applied %d edits to %s", len(in.Edits), in.Path), IsError: false, Diff: strings.Join(diffParts, "\n")}
}

// addedLinesPreview renders the first lines of newly written content as an
// all-added diff, so file creation is visible in the tool footer.
func addedLinesPreview(content string, maxLines int) string {
	if strings.TrimRight(content, "\n") == "" {
		return ""
	}
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	var b strings.Builder
	shown := 0
	for _, line := range lines {
		if shown >= maxLines {
			b.WriteString(ansiDarkGray + "  │ …" + ansiReset + "\n")
			break
		}
		b.WriteString(ansiGreen + "  + " + line + ansiReset + "\n")
		shown++
	}
	return strings.TrimRight(b.String(), "\n")
}

// simpleDiff builds a compact before→after preview for display after an edit.
// Shows up to maxLines of old (removed) and new (added) lines, prefixed with
// ANSI - / + markers. Designed for human readability in the tool footer, not
// for patch application.
func simpleDiff(oldStr, newStr string, maxLines int) string {
	oldLines := strings.Split(strings.TrimRight(oldStr, "\n"), "\n")
	newLines := strings.Split(strings.TrimRight(newStr, "\n"), "\n")

	var b strings.Builder
	oi, ni := 0, 0
	shown := 0
	for oi < len(oldLines) || ni < len(newLines) {
		if shown >= maxLines {
			b.WriteString(ansiDarkGray + "  │ …" + ansiReset + "\n")
			break
		}
		if oi < len(oldLines) {
			b.WriteString(ansiRed + "  - " + oldLines[oi] + ansiReset + "\n")
			oi++
			shown++
		}
		if ni < len(newLines) {
			b.WriteString(ansiGreen + "  + " + newLines[ni] + ansiReset + "\n")
			ni++
			shown++
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
