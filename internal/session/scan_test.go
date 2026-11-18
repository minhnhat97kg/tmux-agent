package session

import (
	"os"
	"path/filepath"
	"testing"
)

// TestScanSessionNames_WithFile checks scanning the case of a valid session name.
func TestScanSessionNames_WithFile(t *testing.T) {
	dir := t.TempDir()
	sessionFile := filepath.Join(dir, "abc-def.json")
	err := os.WriteFile(sessionFile, []byte(`{"sessionId":"abc-def","name":"my-session","pid":1234}`), 0o644)
	if err != nil {
		t.Fatalf("could not write file: %v", err)
	}

	result := ScanSessionNames(dir)
	if result["abc-def"] != "my-session" {
		t.Errorf("expected name=my-session, got %q", result["abc-def"])
	}
}

// TestScanSessionNames_MultiFile checks scanning multiple files at once.
func TestScanSessionNames_MultiFile(t *testing.T) {
	dir := t.TempDir()

	// First file
	err := os.WriteFile(filepath.Join(dir, "session1.json"),
		[]byte(`{"sessionId":"id1","name":"session-one"}`), 0o644)
	if err != nil {
		t.Fatalf("could not write file 1: %v", err)
	}

	// Second file
	err = os.WriteFile(filepath.Join(dir, "session2.json"),
		[]byte(`{"sessionId":"id2","name":"session-two"}`), 0o644)
	if err != nil {
		t.Fatalf("could not write file 2: %v", err)
	}

	result := ScanSessionNames(dir)
	if result["id1"] != "session-one" {
		t.Errorf("expected id1=session-one, got %q", result["id1"])
	}
	if result["id2"] != "session-two" {
		t.Errorf("expected id2=session-two, got %q", result["id2"])
	}
}

// TestScanSessionNames_Skips_NonJson skips a non-.json file.
func TestScanSessionNames_Skips_NonJson(t *testing.T) {
	dir := t.TempDir()

	// Valid .json file
	err := os.WriteFile(filepath.Join(dir, "valid.json"),
		[]byte(`{"sessionId":"id1","name":"valid"}`), 0o644)
	if err != nil {
		t.Fatalf("could not write json file: %v", err)
	}

	// .txt file gets skipped
	err = os.WriteFile(filepath.Join(dir, "ignored.txt"),
		[]byte(`{"sessionId":"id2","name":"ignored"}`), 0o644)
	if err != nil {
		t.Fatalf("could not write txt file: %v", err)
	}

	result := ScanSessionNames(dir)
	if len(result) != 1 {
		t.Errorf("expected 1 entry, got %d", len(result))
	}
	if result["id1"] != "valid" {
		t.Errorf("expected id1=valid, got %q", result["id1"])
	}
}

// TestParseSessionFile_ValidName checks parsing a file with a valid name.
func TestParseSessionFile_ValidName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")
	err := os.WriteFile(path, []byte(`{"sessionId":"abc-def","name":"my-session"}`), 0o644)
	if err != nil {
		t.Fatalf("could not write file: %v", err)
	}

	sessionID, name, ok := parseSessionFile(path)
	if !ok {
		t.Fatal("expected parsing to succeed")
	}
	if sessionID != "abc-def" || name != "my-session" {
		t.Errorf("wrong result: %q, %q", sessionID, name)
	}
}

// TestParseSessionFile_NoName a file with no name field.
func TestParseSessionFile_NoName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")
	err := os.WriteFile(path, []byte(`{"sessionId":"abc-def","pid":1234}`), 0o644)
	if err != nil {
		t.Fatalf("could not write file: %v", err)
	}

	_, _, ok := parseSessionFile(path)
	if ok {
		t.Fatal("expected parsing to fail because there's no name")
	}
}

// TestParseSessionFile_EmptyName a file with an empty name.
func TestParseSessionFile_EmptyName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")
	err := os.WriteFile(path, []byte(`{"sessionId":"abc-def","name":""}`), 0o644)
	if err != nil {
		t.Fatalf("could not write file: %v", err)
	}

	_, _, ok := parseSessionFile(path)
	if ok {
		t.Fatal("expected parsing to fail because name is empty")
	}
}

// TestParseSessionFile_NameIsOnlyWhitespace a file with a whitespace-only name.
func TestParseSessionFile_NameIsOnlyWhitespace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")
	err := os.WriteFile(path, []byte(`{"sessionId":"abc-def","name":"   "}`), 0o644)
	if err != nil {
		t.Fatalf("could not write file: %v", err)
	}

	_, _, ok := parseSessionFile(path)
	if ok {
		t.Fatal("expected parsing to fail because name is whitespace-only")
	}
}

// TestParseSessionFile_BrokenJson an invalid JSON file.
func TestParseSessionFile_BrokenJson(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")
	err := os.WriteFile(path, []byte("not json"), 0o644)
	if err != nil {
		t.Fatalf("could not write file: %v", err)
	}

	_, _, ok := parseSessionFile(path)
	if ok {
		t.Fatal("expected parsing to fail because the JSON is broken")
	}
}

// TestParseSessionFile_FileDoesNotExist a file that doesn't exist.
func TestParseSessionFile_FileDoesNotExist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.json")
	_, _, ok := parseSessionFile(path)
	if ok {
		t.Fatal("expected parsing to fail because the file doesn't exist")
	}
}

// TestParseSessionFile_EmptySessionId a file with an empty sessionId.
func TestParseSessionFile_EmptySessionId(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")
	err := os.WriteFile(path, []byte(`{"sessionId":"","name":"my-session"}`), 0o644)
	if err != nil {
		t.Fatalf("could not write file: %v", err)
	}

	_, _, ok := parseSessionFile(path)
	if ok {
		t.Fatal("expected parsing to fail because sessionId is empty")
	}
}

// TestScanSessionNames_DirectoryDoesNotExist a nonexistent directory returns an empty map.
func TestScanSessionNames_DirectoryDoesNotExist(t *testing.T) {
	nonexistent := filepath.Join(t.TempDir(), "does_not_exist")
	result := ScanSessionNames(nonexistent)
	if len(result) != 0 {
		t.Errorf("expected an empty map, got %v", result)
	}
}

// TestParseSessionFile_WhitespaceTrimmed checks whitespace trimming.
func TestParseSessionFile_WhitespaceTrimmed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")
	err := os.WriteFile(path, []byte(`{"sessionId":"  abc-def  ","name":"  my-session  "}`), 0o644)
	if err != nil {
		t.Fatalf("could not write file: %v", err)
	}

	sessionID, name, ok := parseSessionFile(path)
	if !ok {
		t.Fatal("expected parsing to succeed")
	}
	if sessionID != "abc-def" || name != "my-session" {
		t.Errorf("expected whitespace to be trimmed: %q, %q", sessionID, name)
	}
}

// TestScanSessionNames_EmptyRootDirUsesDefault an empty baseDir uses ~/.claude/sessions.
func TestScanSessionNames_EmptyRootDirUsesDefault(t *testing.T) {
	// When baseDir is empty, it uses ~/.claude/sessions.
	// This test only checks that it doesn't panic when the directory doesn't exist.
	result := ScanSessionNames("")
	// May return an empty map if the directory doesn't exist, or real sessions if it does.
	// Only check that it doesn't panic and returns a map.
	if result == nil {
		t.Fatal("expected a map (possibly empty), not nil")
	}
}

// TestScanSessionNames_SkipsDirectory skips a directory during the scan.
func TestScanSessionNames_SkipsDirectory(t *testing.T) {
	dir := t.TempDir()

	// Create a valid file
	err := os.WriteFile(filepath.Join(dir, "valid.json"),
		[]byte(`{"sessionId":"id1","name":"valid"}`), 0o644)
	if err != nil {
		t.Fatalf("could not write file: %v", err)
	}

	// Create a directory (also with a .json suffix, to check it gets skipped)
	subdir := filepath.Join(dir, "subdir.json")
	err = os.Mkdir(subdir, 0o755)
	if err != nil {
		t.Fatalf("could not create directory: %v", err)
	}

	result := ScanSessionNames(dir)
	if len(result) != 1 {
		t.Errorf("expected 1 entry, got %d", len(result))
	}
	if result["id1"] != "valid" {
		t.Errorf("expected id1=valid, got %q", result["id1"])
	}
}
