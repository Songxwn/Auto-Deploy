package sshx

import (
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/Songxwn/Auto-Deploy/internal/models"
)

func decodeCommand(t *testing.T, enc string) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw)%2 != 0 {
		t.Fatal("odd UTF-16 byte length")
	}
	u16 := make([]uint16, len(raw)/2)
	for i := range u16 {
		u16[i] = binary.LittleEndian.Uint16(raw[i*2:])
	}
	return string(utf16.Decode(u16))
}

func TestEncodeCommand(t *testing.T) {
	for _, script := range []string{"echo hi", "Write-Output '你好'", "Get-Service\nRestart-Service sshd"} {
		got := decodeCommand(t, EncodeCommand(script))
		if got != script {
			t.Fatalf("roundtrip: got %q want %q", got, script)
		}
	}
}

func TestWrapWindows(t *testing.T) {
	script := "Install-WindowsFeature -Name Web-Server"
	wrapped := wrapWindows(script)
	for _, want := range []string{script, "$ErrorActionPreference = 'Stop'", "catch", "exit 1", "$LASTEXITCODE"} {
		if !strings.Contains(wrapped, want) {
			t.Fatalf("wrapped script missing %q:\n%s", want, wrapped)
		}
	}
}

func TestTestScript(t *testing.T) {
	if TestScript(models.OSLinux) != "echo auto-deploy-ok" {
		t.Fatal("linux test script")
	}
	if !strings.Contains(TestScript(models.OSWindows), "auto-deploy-ok") {
		t.Fatal("windows test script")
	}
}
