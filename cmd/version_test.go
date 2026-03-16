package cmd

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestVersionDefaultValue(t *testing.T) {
	if Version != "dev" {
		t.Errorf("Version = %q, want %q", Version, "dev")
	}
}

func TestVersionCmdUse(t *testing.T) {
	if versionCmd.Use != "version" {
		t.Errorf("versionCmd.Use = %q, want %q", versionCmd.Use, "version")
	}
}

func TestCheckForUpdatesDevBuild(t *testing.T) {
	// Version が "dev" の場合スキップされることを確認
	origVersion := Version
	Version = "dev"
	defer func() { Version = origVersion }()

	// 標準出力をキャプチャ
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	checkForUpdates()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	if output == "" {
		t.Fatal("checkForUpdates() produced no output for dev build")
	}
	if !bytes.Contains([]byte(output), []byte("Development build, skipping update check.")) {
		t.Errorf("checkForUpdates() output = %q, want to contain %q", output, "Development build, skipping update check.")
	}
}

func TestVersionCheckFlag(t *testing.T) {
	// --check フラグが登録されていることを確認
	flag := versionCmd.Flags().Lookup("check")
	if flag == nil {
		t.Fatal("versionCmd should have --check flag")
	}
	if flag.DefValue != "false" {
		t.Errorf("--check default = %q, want %q", flag.DefValue, "false")
	}
}
