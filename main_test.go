package main

import "testing"

func TestSourceRootUsesGitHubWorkspace(t *testing.T) {
	t.Setenv("GITHUB_WORKSPACE", "/tmp/workspace")

	got := sourceRoot("repo")
	want := "/tmp/workspace/repo"

	if got != want {
		t.Fatalf("sourceRoot() = %q, want %q", got, want)
	}
}

func TestSourceRootAbsolute(t *testing.T) {
	t.Setenv("GITHUB_WORKSPACE", "/tmp/workspace")

	got := sourceRoot("/repo")
	want := "/tmp/workspace/repo"

	if got != want {
		t.Fatalf("sourceRoot() = %q, want %q", got, want)
	}
}
