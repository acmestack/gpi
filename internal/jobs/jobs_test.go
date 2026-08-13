package jobs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/acmestack/gpi/internal/backend"
	"github.com/acmestack/gpi/internal/state"
)

func TestSubmitPersistsOptimizer(t *testing.T) {
	store, err := state.OpenAt(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	prov, err := backend.New(store, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mgr := New(store, prov)

	taskPath := filepath.Join(t.TempDir(), "task.yaml")
	content := "name: j1\nresources:\n  cpus: 2+\nrun: echo hi\n"
	if err := os.WriteFile(taskPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	job, err := mgr.Submit("j1", taskPath, "", 1, "cost,time")
	if err != nil {
		t.Fatal(err)
	}
	if job.Optimizer != "cost,time" {
		t.Fatalf("optimizer = %q, want cost,time", job.Optimizer)
	}

	// Reload from store to confirm the field persisted.
	got, err := store.GetJob("j1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Optimizer != "cost,time" {
		t.Fatalf("persisted optimizer = %q, want cost,time", got.Optimizer)
	}
}

func TestSubmitDefaultOptimizerEmpty(t *testing.T) {
	store, err := state.OpenAt(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	prov, err := backend.New(store, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mgr := New(store, prov)

	taskPath := filepath.Join(t.TempDir(), "task.yaml")
	if err := os.WriteFile(taskPath, []byte("name: j2\nrun: echo hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	job, err := mgr.Submit("j2", taskPath, "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if job.Optimizer != "" {
		t.Fatalf("default optimizer should be empty (falls back to cost), got %q", job.Optimizer)
	}
}
