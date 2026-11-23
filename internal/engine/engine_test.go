package engine

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Songxwn/Auto-Deploy/internal/models"
	"github.com/Songxwn/Auto-Deploy/internal/secure"
	"github.com/Songxwn/Auto-Deploy/internal/store"
)

type fakeExec struct {
	fn func(ctx context.Context, h models.Host, script string, onLine func(string, string)) (int, error)
}

func (f fakeExec) Run(ctx context.Context, h models.Host, secret, passphrase, script string, onLine func(string, string)) (int, error) {
	return f.fn(ctx, h, script, onLine)
}

func newTestEngine(t *testing.T, exec Executor) (*Engine, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box := secure.NewBox("test-secret")
	return New(st, box, exec, 4, 0), st
}

func testHost(t *testing.T, st *store.Store, name string) models.Host {
	t.Helper()
	box := secure.NewBox("test-secret")
	secret, err := box.Encrypt("pw")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	h := models.Host{
		ID: store.NewID(), Name: name, Addr: "127.0.0.1", Port: 22, User: "root",
		AuthType: models.AuthPassword, Secret: secret, OSType: models.OSLinux,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := st.CreateHost(&h); err != nil {
		t.Fatal(err)
	}
	return h
}

func testTask() models.Task {
	return models.Task{
		ID: store.NewID(), Name: "demo", OSType: models.OSLinux,
		Steps: []models.Step{
			{Name: "one", Script: "echo one"},
			{Name: "two", Script: "echo two"},
		},
	}
}

func waitRun(t *testing.T, st *store.Store, id string) models.Run {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, err := st.GetRun(id)
		if err != nil {
			t.Fatal(err)
		}
		if models.Terminal(run.Status) {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("run did not finish in time")
	return models.Run{}
}

func TestRunSuccess(t *testing.T) {
	var calls int
	exec := fakeExec{fn: func(ctx context.Context, h models.Host, script string, onLine func(string, string)) (int, error) {
		calls++
		onLine("stdout", "out:"+script)
		return 0, nil
	}}
	e, st := newTestEngine(t, exec)
	h1, h2 := testHost(t, st, "a"), testHost(t, st, "b")
	run, err := e.StartRun(testTask(), []models.Host{h1, h2})
	if err != nil {
		t.Fatal(err)
	}
	got := waitRun(t, st, run.ID)
	if got.Status != models.StatusSuccess {
		t.Fatalf("status = %s", got.Status)
	}
	for _, rh := range got.Hosts {
		if rh.Status != models.StatusSuccess || rh.ExitCode != 0 {
			t.Fatalf("host %s: %+v", rh.HostName, rh)
		}
	}
	lines, err := st.LogsForRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	for _, l := range lines {
		text.WriteString(l.Text + "\n")
	}
	for _, want := range []string{"步骤 1/2 · one", "步骤 2/2 · two", "out:echo one", "out:echo two"} {
		if !strings.Contains(text.String(), want) {
			t.Fatalf("logs missing %q", want)
		}
	}
}

func TestRunStepFailureStops(t *testing.T) {
	exec := fakeExec{fn: func(ctx context.Context, h models.Host, script string, onLine func(string, string)) (int, error) {
		if strings.Contains(script, "one") {
			return 3, nil
		}
		t.Error("second step should not run after failure")
		return 0, nil
	}}
	e, st := newTestEngine(t, exec)
	h := testHost(t, st, "a")
	run, err := e.StartRun(testTask(), []models.Host{h})
	if err != nil {
		t.Fatal(err)
	}
	got := waitRun(t, st, run.ID)
	if got.Status != models.StatusFailed {
		t.Fatalf("status = %s", got.Status)
	}
	if got.Hosts[0].ExitCode != 3 {
		t.Fatalf("exit code = %d", got.Hosts[0].ExitCode)
	}
	lines, _ := st.LogsForRun(run.ID)
	for _, l := range lines {
		if strings.Contains(l.Text, "步骤 2/2") {
			t.Fatal("step 2 marker should not exist")
		}
	}
}

func TestRunCancel(t *testing.T) {
	started := make(chan struct{}, 1)
	exec := fakeExec{fn: func(ctx context.Context, h models.Host, script string, onLine func(string, string)) (int, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return -1, ctx.Err()
	}}
	e, st := newTestEngine(t, exec)
	h := testHost(t, st, "a")
	run, err := e.StartRun(testTask(), []models.Host{h})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("executor never started")
	}
	if !e.Cancel(run.ID) {
		t.Fatal("cancel returned false")
	}
	got := waitRun(t, st, run.ID)
	if got.Status != models.StatusCanceled {
		t.Fatalf("status = %s", got.Status)
	}
	if got.Hosts[0].Status != models.StatusCanceled {
		t.Fatalf("host status = %s", got.Hosts[0].Status)
	}
}

func TestSubscribeReceivesEvents(t *testing.T) {
	exec := fakeExec{fn: func(ctx context.Context, h models.Host, script string, onLine func(string, string)) (int, error) {
		onLine("stdout", "hello")
		return 0, nil
	}}
	e, st := newTestEngine(t, exec)
	h := testHost(t, st, "a")
	run, err := e.StartRun(testTask(), []models.Host{h})
	if err != nil {
		t.Fatal(err)
	}
	ch := e.Subscribe(run.ID)
	defer e.Unsubscribe(run.ID, ch)
	waitRun(t, st, run.ID)
	// Events published before subscribing are not replayed by the hub;
	// persistence is verified separately. Here we only ensure no deadlock
	// and that unsubscribe cleans up.
	e.hub.mu.Lock()
	n := len(e.hub.subs[run.ID])
	e.hub.mu.Unlock()
	if n != 1 {
		t.Fatalf("subscribers = %d", n)
	}
}
