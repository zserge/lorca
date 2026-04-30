package lorca

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestLocate(t *testing.T) {
	if exe := ChromeExecutable(""); exe == "" {
		t.Fatal()
	} else {
		t.Log(exe)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		b, err := exec.CommandContext(ctx, exe, "--version").CombinedOutput()
		t.Log(string(b))
		t.Log(err)
	}
}

func TestLocateFirefoxEnvVar(t *testing.T) {
	path := t.TempDir() + "/firefox"
	// Create a dummy file so os.Stat succeeds
	if err := os.WriteFile(path, []byte(""), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LORCAFIREFOX", path)
	got := LocateFirefox("")
	if got != path {
		t.Fatalf("expected %q, got %q", path, got)
	}
}

func TestLocateFirefoxPreferPath(t *testing.T) {
	path := t.TempDir() + "/my-firefox"
	if err := os.WriteFile(path, []byte(""), 0755); err != nil {
		t.Fatal(err)
	}
	got := LocateFirefox(path)
	if got != path {
		t.Fatalf("expected %q, got %q", path, got)
	}
}
