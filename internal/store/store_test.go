package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Songxwn/Auto-Deploy/internal/models"
	"github.com/Songxwn/Auto-Deploy/internal/templates"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestHostCRUD(t *testing.T) {
	s := openTest(t)
	now := time.Now()
	h := models.Host{
		ID: NewID(), Name: "web-01", Addr: "10.0.0.1", Port: 22, User: "root",
		AuthType: models.AuthPassword, Secret: "enc", OSType: models.OSLinux,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateHost(&h); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetHost(h.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "web-01" || got.Secret != "enc" {
		t.Fatalf("unexpected host: %+v", got)
	}
	got.Name = "web-02"
	got.UpdatedAt = time.Now()
	if err := s.UpdateHost(&got); err != nil {
		t.Fatal(err)
	}
	hosts, err := s.ListHosts()
	if err != nil || len(hosts) != 1 || hosts[0].Name != "web-02" {
		t.Fatalf("list: %v %+v", err, hosts)
	}
	if err := s.DeleteHost(h.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetHost(h.ID); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestTaskCRUD(t *testing.T) {
	s := openTest(t)
	now := time.Now()
	task := models.Task{
		ID: NewID(), Name: "init", OSType: models.OSLinux,
		Steps:     []models.Step{{Name: "install", Script: "apt-get install -y nginx"}},
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Steps) != 1 || got.Steps[0].Script != task.Steps[0].Script {
		t.Fatalf("steps mismatch: %+v", got)
	}
	got.Steps = append(got.Steps, models.Step{Name: "start", Script: "systemctl start nginx"})
	if err := s.UpdateTask(&got); err != nil {
		t.Fatal(err)
	}
	tasks, err := s.ListTasks()
	if err != nil || len(tasks) != 1 || len(tasks[0].Steps) != 2 {
		t.Fatalf("list: %v %+v", err, tasks)
	}
	if err := s.DeleteTask(task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetTask(task.ID); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestRunAndLogs(t *testing.T) {
	s := openTest(t)
	now := time.Now()
	run := models.Run{
		ID: NewID(), TaskID: "t1", TaskName: "init", Status: models.StatusRunning,
		CreatedAt: now, StartedAt: now,
		Hosts: []models.RunHost{
			{ID: NewID(), HostID: "h1", HostName: "web-01", Status: models.StatusPending, ExitCode: -1},
		},
	}
	run.Hosts[0].RunID = run.ID
	if err := s.CreateRun(&run); err != nil {
		t.Fatal(err)
	}
	rh := run.Hosts[0]
	if err := s.StartRunHost(rh.ID, now); err != nil {
		t.Fatal(err)
	}
	for i := int64(1); i <= 3; i++ {
		if err := s.AppendLog(models.LogLine{RunHostID: rh.ID, Seq: i, TS: now, Stream: "stdout", Text: "line"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.FinishRunHost(rh.ID, models.StatusSuccess, 0, now); err != nil {
		t.Fatal(err)
	}
	fin := now.Add(time.Second)
	if err := s.UpdateRunStatus(run.ID, models.StatusSuccess, &fin); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != models.StatusSuccess || got.FinishedAt == nil {
		t.Fatalf("run: %+v", got)
	}
	if len(got.Hosts) != 1 || got.Hosts[0].Status != models.StatusSuccess || got.Hosts[0].ExitCode != 0 {
		t.Fatalf("run host: %+v", got.Hosts)
	}
	lines, err := s.LogsForRun(run.ID)
	if err != nil || len(lines) != 3 {
		t.Fatalf("logs: %v len=%d", err, len(lines))
	}
	runs, err := s.ListRuns(10)
	if err != nil || len(runs) != 1 {
		t.Fatalf("list runs: %v %+v", err, runs)
	}
}

func TestTemplateCRUDAndSeed(t *testing.T) {
	s := openTest(t)
	builtIns := templates.BuiltIn()
	if err := s.SeedTemplates(builtIns); err != nil {
		t.Fatal(err)
	}
	// Seeding is idempotent.
	if err := s.SeedTemplates(builtIns); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListTemplates()
	if err != nil || len(list) != len(builtIns) {
		t.Fatalf("seed list: %v len=%d want=%d", err, len(list), len(builtIns))
	}

	now := time.Now()
	tpl := models.Template{
		ID: NewID(), Name: "my-tpl", Description: "desc", OSType: models.OSLinux,
		Category: models.CatCustom, Script: "echo hi", CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateTemplate(&tpl); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTemplate(tpl.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "my-tpl" || got.BuiltIn || got.Script != "echo hi" {
		t.Fatalf("unexpected template: %+v", got)
	}
	got.Name = "renamed"
	got.UpdatedAt = time.Now()
	if err := s.UpdateTemplate(&got); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteTemplate(tpl.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetTemplate(tpl.ID); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := s.DeleteTemplate(tpl.ID); err != ErrNotFound {
		t.Fatalf("delete missing: want ErrNotFound, got %v", err)
	}
}
