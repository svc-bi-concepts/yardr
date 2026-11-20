package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/storetest"
)

// mcpStdioHelperEnv marks the child process TestMCPStdoutIsOnlyJSONRPC
// starts: the test binary run as yardr mcp, over the pipes the parent
// holds. The child is a process of its own because only a real fd 1 that
// is a pipe shows what a client reads: the tests above replace the
// transport with their own pipes (mcpServe).
const mcpStdioHelperEnv = "YARDR_TEST_MCP_STDIO_HELPER"

// TestMCPStdoutIsOnlyJSONRPC is yard-d0fg's finding: over a pipe, stdout
// of yardr mcp ends with terminal escape codes after the last JSON-RPC
// line, which a strict MCP client may reject. Every byte the child writes
// to stdout, through its exit, must be a JSON-RPC message on a line of its
// own, and no escape byte may appear. The client holds stdin open until
// each answer is read: a stdin closed early ends the server before its
// answer is written.
func TestMCPStdoutIsOnlyJSONRPC(t *testing.T) {
	t.Parallel()
	cmd := exec.Command(os.Args[0], "-test.run=^TestMCPStdioHelper$")
	tmp := t.TempDir()
	cmd.Env = append(os.Environ(), mcpStdioHelperEnv+"=1", "TMPDIR="+tmp)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// A failed step leaves the child waiting on stdin: kill it, so the
	// test ends rather than hangs. Once it has exited, Kill does nothing.
	defer func() { _ = cmd.Process.Kill() }()

	lines := make(chan string, 64)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()
	next := func() string {
		t.Helper()
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatalf("yardr mcp's stdout ended before the answer; stderr: %s", stderr.String())
			}
			return line
		case <-time.After(60 * time.Second):
			t.Fatalf("yardr mcp gave no answer within 60s; stderr: %s", stderr.String())
		}
		return ""
	}
	// jsonLine holds one line of stdout to the plain-JSON bar: a whole
	// JSON-RPC message, with no control code before, after or within it.
	jsonLine := func(line string) {
		t.Helper()
		if strings.ContainsRune(line, 0x1b) {
			t.Errorf("stdout carries an escape code: %q", line)
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("stdout line is not one JSON-RPC message: %q (%v)", line, err)
		}
		if msg["jsonrpc"] != "2.0" {
			t.Fatalf("stdout line is not JSON-RPC 2.0: %q", line)
		}
	}
	send := func(line string) {
		t.Helper()
		if _, err := fmt.Fprintln(stdin, line); err != nil {
			t.Fatal(err)
		}
	}

	send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"stand-in","version":"1"}}}`)
	jsonLine(next())
	send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	jsonLine(next())
	send(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"what_waits_for_me","arguments":{}}}`)
	jsonLine(next())

	// The client leaves: the server ends, and everything it wrote through
	// its exit is read here, up to the close of its stdout.
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	for line := range lines {
		jsonLine(line)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("yardr mcp exited with %v; stderr: %s", err, stderr.String())
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("helper left temporary fixtures behind: %v", entries)
	}
}

// TestMCPStdioHelper is the child of TestMCPStdoutIsOnlyJSONRPC. It runs
// only when that test starts it: a yard of one user, served as yardr mcp
// on the process's own stdin and stdout.
func TestMCPStdioHelper(t *testing.T) {
	t.Parallel()
	if os.Getenv(mcpStdioHelperEnv) == "" {
		t.Skip("runs only as the child of TestMCPStdoutIsOnlyJSONRPC")
	}
	defer func() { os.Stdout = os.Stderr }()
	home := t.TempDir()
	st := storetest.NewIn(t, home)
	ctx := t.Context()
	if _, err := st.AddUser(ctx, "ada", "ada"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting(ctx, store.OperatorSetting, "ada", "ada"); err != nil {
		t.Fatal(err)
	}
	a := &app{st: st, actor: "ada", env: testEnv(t, home)}
	cmd := a.mcpCmd()
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
}
