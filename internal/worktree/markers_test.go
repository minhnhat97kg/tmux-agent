package worktree

import "testing"

func TestParseSpawnMarkersAllFieldsPopulated(t *testing.T) {
	raw := "1\n/repo\n/repo/.worktrees/foo\nagent/foo\n@42\n"
	m := ParseSpawnMarkers(raw)
	if !m.Spawned {
		t.Error("Spawned want true")
	}
	if m.FromRepo != "/repo" {
		t.Errorf("FromRepo = %q, want /repo", m.FromRepo)
	}
	if m.WorktreePath != "/repo/.worktrees/foo" {
		t.Errorf("WorktreePath = %q, want /repo/.worktrees/foo", m.WorktreePath)
	}
	if m.Branch != "agent/foo" {
		t.Errorf("Branch = %q, want agent/foo", m.Branch)
	}
	if m.WindowID != "@42" {
		t.Errorf("WindowID = %q, want @42", m.WindowID)
	}
	if !m.IsSpawned() {
		t.Error("IsSpawned() want true")
	}
}

func TestParseSpawnMarkersMissingTrailingFieldsDefaultToEmpty(t *testing.T) {
	m := ParseSpawnMarkers("1\n/repo\n")
	if !m.Spawned {
		t.Error("Spawned want true")
	}
	if m.FromRepo != "/repo" {
		t.Errorf("FromRepo = %q, want /repo", m.FromRepo)
	}
	if m.WorktreePath != "" || m.Branch != "" || m.WindowID != "" {
		t.Errorf("missing field must be empty, got %+v", m)
	}
}

func TestParseSpawnMarkersEmptyInputYieldsDefault(t *testing.T) {
	m := ParseSpawnMarkers("")
	if m != (SpawnMarkers{}) {
		t.Errorf("ParseSpawnMarkers(empty) = %+v, want zero value", m)
	}
	if m.IsSpawned() {
		t.Error("IsSpawned() want false")
	}
}

func TestIsSpawnedRequiresBothFlagAndRepo(t *testing.T) {
	m := SpawnMarkers{Spawned: true}
	if m.IsSpawned() {
		t.Error("flag alone, no repo, must not count as spawned")
	}
	m.FromRepo = "/repo"
	if !m.IsSpawned() {
		t.Error("both flag and repo must count as spawned")
	}
	m.Spawned = false
	if m.IsSpawned() {
		t.Error("repo alone, no flag, must not count as spawned")
	}
}

func TestParseSpawnMarkersNonOneFlagIsFalse(t *testing.T) {
	// Only the literal "1" counts as spawned; any other value (including
	// "true", "yes", "0") reads as false.
	if ParseSpawnMarkers("true\n/repo\n").Spawned {
		t.Error(`"true" is not "1", Spawned want false`)
	}
	if ParseSpawnMarkers("0\n/repo\n").Spawned {
		t.Error(`"0" is not "1", Spawned want false`)
	}
	if ParseSpawnMarkers("\n/repo\n").Spawned {
		t.Error(`empty line is not "1", Spawned want false`)
	}
}
