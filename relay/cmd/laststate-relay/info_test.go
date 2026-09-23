package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestInfoCmdDoesNotPrintBanner(t *testing.T) {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
	}()

	err = infoCmd(nil)
	w.Close()
	if err != nil {
		t.Fatalf("infoCmd returned error: %v", err)
	}

	out, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("read stdout: %v", readErr)
	}

	if strings.Contains(string(out), "LAST STATE") {
		t.Fatalf("infoCmd unexpectedly printed a banner:\n%s", string(out))
	}
}
