package backend

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/acmestack/gpi/internal/optimizer"
	"github.com/acmestack/gpi/internal/state"
	"github.com/acmestack/gpi/internal/task"
)

func testStore(t *testing.T) *state.Store {
	t.Helper()
	store, err := state.OpenAt(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestLocalBackend(t *testing.T) {
	store := testStore(t)
	mgr, err := New(store, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ts := &task.Task{
		Name:    "local-job",
		Backend: task.BackendLocal,
		Run:     "echo hello-local",
	}
	launch := &optimizer.Launch{Cloud: task.BackendLocal, NumNodes: 1}
	cluster, err := mgr.Launch(context.Background(), ts.Name, ts, launch)
	if err != nil {
		t.Fatal(err)
	}
	if cluster.Backend != task.BackendLocal {
		t.Fatalf("backend = %q, want local", cluster.Backend)
	}
	code, err := mgr.RunTask(context.Background(), ts.Name, ts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("run exit = %d, want 0", code)
	}
	if err := mgr.Down(context.Background(), ts.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetCluster(ts.Name); err == nil {
		t.Fatal("cluster should be removed after down")
	}
}

func TestLocalBackendFailingRun(t *testing.T) {
	store := testStore(t)
	mgr, err := New(store, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ts := &task.Task{
		Name:    "local-fail",
		Backend: task.BackendLocal,
		Run:     "exit 7",
	}
	code, err := mgr.RunTask(context.Background(), "nope", ts, nil)
	if err == nil {
		t.Fatal("expected error for missing cluster")
	}
	_ = code

	// Launch then run a failing command.
	launch := &optimizer.Launch{Cloud: task.BackendLocal, NumNodes: 1}
	if _, err := mgr.Launch(context.Background(), ts.Name, ts, launch); err != nil {
		t.Fatal(err)
	}
	code, err = mgr.RunTask(context.Background(), ts.Name, ts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if code != 7 {
		t.Fatalf("exit = %d, want 7", code)
	}
}

func TestUnknownBackendTask(t *testing.T) {
	store := testStore(t)
	mgr, err := New(store, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ts := &task.Task{Name: "x", Backend: "nope", Run: "true"}
	if _, err := mgr.Launch(context.Background(), "x", ts, nil); err == nil {
		t.Fatal("expected unknown backend error")
	}
}

func TestExistingBackendValidation(t *testing.T) {
	if _, err := task.Parse([]byte("backend: existing\nrun: echo hi\n")); err == nil {
		t.Fatal("existing backend without ssh block should error")
	}
	if _, err := task.Parse([]byte("backend: docker\nrun: echo hi\n")); err == nil {
		t.Fatal("docker backend without image should error")
	}
	if _, err := task.Parse([]byte("backend: existing\nssh:\n  host: 1.2.3.4\nrun: echo hi\n")); err != nil {
		t.Fatalf("valid existing backend should parse: %v", err)
	}
	if _, err := task.Parse([]byte("backend: docker\ndocker:\n  image: python:3.11\nrun: echo hi\n")); err != nil {
		t.Fatalf("valid docker backend should parse: %v", err)
	}
}

func TestLaunchRecordsYAMLHistoryEvent(t *testing.T) {
	store := testStore(t)
	mgr, err := New(store, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ts := &task.Task{
		Name:    "rec-job",
		Backend: task.BackendLocal,
		Run:     "echo hi",
	}
	launch := &optimizer.Launch{Cloud: task.BackendLocal, NumNodes: 1}
	if _, err := mgr.Launch(context.Background(), ts.Name, ts, launch); err != nil {
		t.Fatal(err)
	}

	y, err := store.GetClusterYAML(ts.Name)
	if err != nil {
		t.Fatalf("yaml snapshot missing: %v", err)
	}
	if y.YAML == "" {
		t.Fatal("yaml snapshot empty")
	}
	if len(store.ListClusterHistory()) != 1 {
		t.Fatalf("history missing, got %d", len(store.ListClusterHistory()))
	}
	events := store.ListClusterEventsFor(ts.Name)
	if len(events) != 1 || events[0].Type != state.EventLaunch {
		t.Fatalf("launch event missing: %+v", events)
	}

	if err := mgr.Down(context.Background(), ts.Name); err != nil {
		t.Fatal(err)
	}
	// History and events survive cluster teardown.
	if len(store.ListClusterHistory()) != 1 {
		t.Fatal("history should survive down")
	}
	if len(store.ListClusterEventsFor(ts.Name)) != 1 {
		t.Fatal("events should survive down")
	}
}
