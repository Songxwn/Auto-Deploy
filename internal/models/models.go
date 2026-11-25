// Package models defines the core data types shared across the application.
package models

import "time"

// Supported target operating systems.
const (
	OSLinux   = "linux"
	OSWindows = "windows"
)

// Host authentication methods.
const (
	AuthPassword = "password"
	AuthKey      = "key"
)

// Run / run-host lifecycle states.
const (
	StatusPending  = "pending"
	StatusRunning  = "running"
	StatusSuccess  = "success"
	StatusFailed   = "failed"
	StatusCanceled = "canceled"
)

// Terminal reports whether a status is final.
func Terminal(status string) bool {
	return status == StatusSuccess || status == StatusFailed || status == StatusCanceled
}

// Host is an SSH-managed target machine.
type Host struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Addr       string    `json:"addr"`
	Port       int       `json:"port"`
	User       string    `json:"user"`
	AuthType   string    `json:"auth_type"`
	Secret     string    `json:"-"` // password or private key, encrypted at rest
	Passphrase string    `json:"-"` // private key passphrase, encrypted at rest
	OSType     string    `json:"os_type"`
	Tags       string    `json:"tags"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Step is one named script inside a task.
type Step struct {
	Name   string `json:"name"`
	Script string `json:"script"`
}

// Template categories used to group the template library.
const (
	CatWeb      = "web"
	CatDatabase = "database"
	CatRuntime  = "runtime"
	CatSystem   = "system"
	CatNetwork  = "network"
	CatCustom   = "custom"
)

// Template is a reusable deployment script that can be inserted into tasks.
type Template struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	OSType      string    `json:"os_type"`
	Category    string    `json:"category"`
	Script      string    `json:"script"`
	BuiltIn     bool      `json:"built_in"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Task is an ordered deployment plan executed over SSH.
type Task struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	OSType      string    `json:"os_type"`
	Steps       []Step    `json:"steps"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Run is one execution of a task against a set of hosts.
type Run struct {
	ID         string     `json:"id"`
	TaskID     string     `json:"task_id"`
	TaskName   string     `json:"task_name"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Hosts      []RunHost  `json:"hosts"`
}

// RunHost tracks the execution of a run on a single host.
type RunHost struct {
	ID         string     `json:"id"`
	RunID      string     `json:"run_id"`
	HostID     string     `json:"host_id"`
	HostName   string     `json:"host_name"`
	Status     string     `json:"status"`
	ExitCode   int        `json:"exit_code"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// LogLine is a single captured output line of a run-host.
type LogLine struct {
	RunHostID string    `json:"run_host_id"`
	Seq       int64     `json:"seq"`
	TS        time.Time `json:"ts"`
	Stream    string    `json:"stream"` // stdout | stderr | system
	Text      string    `json:"text"`
}
