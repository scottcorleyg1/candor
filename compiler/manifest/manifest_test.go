// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0

package manifest_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/candor-core/candor/compiler/manifest"
)

func writeManifest(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Candor.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadBasic(t *testing.T) {
	path := writeManifest(t, `
[package]
name    = "hello"
version = "0.1.0"
entry   = "src/main.cnd"
`)
	m, err := manifest.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "hello" {
		t.Errorf("Name = %q, want %q", m.Name, "hello")
	}
	if m.Version != "0.1.0" {
		t.Errorf("Version = %q, want %q", m.Version, "0.1.0")
	}
	if m.Entry != "src/main.cnd" {
		t.Errorf("Entry = %q, want %q", m.Entry, "src/main.cnd")
	}
}

func TestLoadBuildSection(t *testing.T) {
	path := writeManifest(t, `
[package]
name = "myapp"

[build]
output  = "bin/myapp"
sources = ["src/main.cnd", "src/lib.cnd"]
`)
	m, err := manifest.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.Output != "bin/myapp" {
		t.Errorf("Output = %q", m.Output)
	}
	if len(m.Sources) != 2 {
		t.Fatalf("Sources len = %d, want 2", len(m.Sources))
	}
	if m.Sources[0] != "src/main.cnd" {
		t.Errorf("Sources[0] = %q", m.Sources[0])
	}
}

func TestLoadMissingName(t *testing.T) {
	path := writeManifest(t, `
[package]
version = "0.1.0"
`)
	_, err := manifest.Load(path)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestLoadWithComments(t *testing.T) {
	path := writeManifest(t, `
## Project manifest
[package]
name    = "proj"  # inline comment
version = "1.0.0"
`)
	m, err := manifest.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "proj" {
		t.Errorf("Name = %q, want %q", m.Name, "proj")
	}
}

func TestOutputPath(t *testing.T) {
	path := writeManifest(t, `[package]
name = "app"
`)
	m, err := manifest.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	out := m.OutputPath(false)
	if filepath.Base(out) != "app" {
		t.Errorf("OutputPath = %q, want basename 'app'", out)
	}
}

func TestFindManifest(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	tomlPath := filepath.Join(dir, "Candor.toml")
	if err := os.WriteFile(tomlPath, []byte("[package]\nname=\"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	found, err := manifest.FindManifest(sub)
	if err != nil {
		t.Fatal(err)
	}
	if found != tomlPath {
		t.Errorf("FindManifest = %q, want %q", found, tomlPath)
	}
}

// ── M8.1: [dependencies] and lock file ───────────────────────────────────────

func TestLoadDependencies(t *testing.T) {
	path := writeManifest(t, `
[package]
name = "myapp"

[dependencies]
mylib      = "path:../mylib"
remote-pkg = "git:https://github.com/user/repo@v1.0.0"
`)
	m, err := manifest.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Deps) != 2 {
		t.Fatalf("Deps len = %d, want 2", len(m.Deps))
	}
	if m.Deps[0].Name != "mylib" {
		t.Errorf("Deps[0].Name = %q", m.Deps[0].Name)
	}
	if m.Deps[0].Source != "path:../mylib" {
		t.Errorf("Deps[0].Source = %q", m.Deps[0].Source)
	}
	if m.Deps[1].Name != "remote-pkg" {
		t.Errorf("Deps[1].Name = %q", m.Deps[1].Name)
	}
}

func TestParseDep(t *testing.T) {
	kind, loc, ver := manifest.ParseDep("path:../mylib")
	if kind != manifest.DepPath || loc != "../mylib" || ver != "" {
		t.Errorf("ParseDep path: got kind=%d loc=%q ver=%q", kind, loc, ver)
	}

	kind, loc, ver = manifest.ParseDep("git:https://github.com/user/repo@v1.2.3")
	if kind != manifest.DepGit || loc != "https://github.com/user/repo" || ver != "v1.2.3" {
		t.Errorf("ParseDep git: got kind=%d loc=%q ver=%q", kind, loc, ver)
	}

	kind, loc, ver = manifest.ParseDep("git:https://github.com/user/repo")
	if kind != manifest.DepGit || loc != "https://github.com/user/repo" || ver != "" {
		t.Errorf("ParseDep git no version: got kind=%d loc=%q ver=%q", kind, loc, ver)
	}

	kind, _, _ = manifest.ParseDep("unknown:something")
	if kind != manifest.DepUnknown {
		t.Errorf("ParseDep unknown: got kind=%d", kind)
	}
}

func TestResolvedDirPath(t *testing.T) {
	dir := t.TempDir()
	libDir := filepath.Join(dir, "mylib")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tomlPath := filepath.Join(dir, "app", "Candor.toml")
	if err := os.MkdirAll(filepath.Dir(tomlPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tomlPath, []byte("[package]\nname=\"app\"\n[dependencies]\nmylib = \"path:../mylib\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(tomlPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Deps) != 1 {
		t.Fatalf("Deps len = %d", len(m.Deps))
	}
	resolved, err := m.ResolvedDir(m.Deps[0])
	if err != nil {
		t.Fatal(err)
	}
	if resolved != libDir {
		t.Errorf("ResolvedDir = %q, want %q", resolved, libDir)
	}
}

func TestLockFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "Candor.lock")

	lf := &manifest.LockFile{
		Packages: []manifest.LockedPackage{
			{Name: "mylib", Source: "path:../mylib", Resolved: "/abs/mylib"},
			{Name: "remote", Source: "git:https://github.com/x/y@v1.0.0", Resolved: "/cache/remote/v1.0.0", Rev: "abc123"},
		},
	}
	if err := manifest.WriteLock(lockPath, lf); err != nil {
		t.Fatal(err)
	}

	lf2, err := manifest.LoadLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(lf2.Packages) != 2 {
		t.Fatalf("Packages len = %d, want 2", len(lf2.Packages))
	}
	if lf2.Packages[0].Name != "mylib" {
		t.Errorf("Packages[0].Name = %q", lf2.Packages[0].Name)
	}
	if lf2.Packages[1].Rev != "abc123" {
		t.Errorf("Packages[1].Rev = %q", lf2.Packages[1].Rev)
	}
	p := lf2.Find("remote")
	if p == nil || p.Resolved != "/cache/remote/v1.0.0" {
		t.Errorf("Find(remote) = %v", p)
	}
}

func TestLoadLockMissing(t *testing.T) {
	// LoadLock on a non-existent file should return empty lock, not error.
	lf, err := manifest.LoadLock("/nonexistent/path/Candor.lock")
	if err != nil {
		t.Fatalf("expected no error for missing lock, got %v", err)
	}
	if len(lf.Packages) != 0 {
		t.Errorf("expected 0 packages, got %d", len(lf.Packages))
	}
}

func TestDepSourceFiles(t *testing.T) {
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write two .cnd files and one .txt that should be ignored.
	for _, name := range []string{"lib.cnd", "util.cnd"} {
		if err := os.WriteFile(filepath.Join(srcDir, name), []byte("fn f() -> unit { return unit }"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(srcDir, "README.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := manifest.DepSourceFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Errorf("DepSourceFiles returned %d files, want 2: %v", len(files), files)
	}
}
