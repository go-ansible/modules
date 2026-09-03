package modules

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// nagiosTestFifo creates a real FIFO in dir and returns a function that
// reads every line written to it, delivering them on the returned
// channel — nagios's command-file write is a pure local file operation
// with no root/daemon dependency, so (matching this project's own
// testing convention for such modules — see fakeconn_test.go's own doc
// comment) this test exercises it against a real Connection rather than
// a scripted fakeConn.
//
// moduleNagios's own nagiosWriteCommand opens, writes, and closes the
// FIFO separately for EACH command line (`cat > cmdfile`, once per
// line) — matching real nagios's own _write_command exactly, and the
// standard, documented way of feeding Nagios's command pipe. A writer
// blocks until a reader has the FIFO open; a reader opened O_RDONLY
// sees EOF (and would need to re-open to see more) once ALL writers
// have closed. This helper instead opens the FIFO O_RDWR: on a FIFO
// that never blocks, and — because the fd itself then also counts as a
// (silent) writer — reads never observe EOF between two separate
// external writers, so one long-lived Scanner can read every command
// line across the whole test without a reopen loop. (A first draft
// without this read O_RDONLY-to-EOF-then-stop; a second write in the
// same test then blocked forever with no reader left to unblock it —
// caught by a goroutine-dump timeout, not a bug in nagios.go itself.)
func nagiosTestFifo(t *testing.T, dir, name string) (path string, lines <-chan string) {
	t.Helper()
	path = filepath.Join(dir, name)
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open fifo O_RDWR: %v", err)
	}
	t.Cleanup(func() { f.Close() })
	ch := make(chan string, 16)
	go func() {
		defer close(ch)
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			ch <- sc.Text()
		}
	}()
	return path, ch
}

func TestModuleNagiosDowntimeHost(t *testing.T) {
	dir := t.TempDir()
	cmdfile, lines := nagiosTestFifo(t, dir, "nagios.cmd")
	conn := local()

	res, err := moduleNagios(context.Background(), conn, map[string]any{
		"action":  "downtime",
		"host":    "web01",
		"service": "host",
		"minutes": 60,
		"cmdfile": cmdfile,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	line := <-lines
	if !strings.Contains(line, "SCHEDULE_HOST_DOWNTIME;web01;") {
		t.Fatalf("line = %q", line)
	}
}

func TestModuleNagiosDowntimeServices(t *testing.T) {
	dir := t.TempDir()
	cmdfile, lines := nagiosTestFifo(t, dir, "nagios.cmd")
	conn := local()

	res, err := moduleNagios(context.Background(), conn, map[string]any{
		"action":   "downtime",
		"host":     "web01",
		"services": []any{"httpd", "nfs"},
		"minutes":  30,
		"cmdfile":  cmdfile,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	first := <-lines
	second := <-lines
	if !strings.Contains(first, "SCHEDULE_SVC_DOWNTIME;web01;httpd;") {
		t.Fatalf("first = %q", first)
	}
	if !strings.Contains(second, "SCHEDULE_SVC_DOWNTIME;web01;nfs;") {
		t.Fatalf("second = %q", second)
	}
	cmds, ok := res.Extra["nagios_commands"].([]string)
	if !ok || len(cmds) != 2 {
		t.Fatalf("nagios_commands = %#v", res.Extra["nagios_commands"])
	}
}

func TestModuleNagiosRawCommand(t *testing.T) {
	dir := t.TempDir()
	cmdfile, lines := nagiosTestFifo(t, dir, "nagios.cmd")
	conn := local()

	res, err := moduleNagios(context.Background(), conn, map[string]any{
		"action":  "command",
		"command": "DISABLE_FAILURE_PREDICTION",
		"cmdfile": cmdfile,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	line := <-lines
	if !strings.HasSuffix(line, "DISABLE_FAILURE_PREDICTION") {
		t.Fatalf("line = %q", line)
	}
}

func TestModuleNagiosSilenceNagios(t *testing.T) {
	dir := t.TempDir()
	cmdfile, lines := nagiosTestFifo(t, dir, "nagios.cmd")
	conn := local()

	res, err := moduleNagios(context.Background(), conn, map[string]any{
		"action":  "silence_nagios",
		"cmdfile": cmdfile,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	line := <-lines
	if !strings.HasSuffix(line, "DISABLE_NOTIFICATIONS") {
		t.Fatalf("line = %q", line)
	}
}

func TestModuleNagiosCmdfileNotFifo(t *testing.T) {
	dir := t.TempDir()
	regular := filepath.Join(dir, "notafifo")
	if err := os.WriteFile(regular, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleNagios(context.Background(), conn, map[string]any{
		"action":  "silence_nagios",
		"cmdfile": regular,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a cmdfile that isn't a FIFO")
	}
}

func TestModuleNagiosMissingRequiredArgs(t *testing.T) {
	conn := local()
	if _, err := moduleNagios(context.Background(), conn, map[string]any{"action": "downtime"}); err == nil {
		t.Fatal("want error for downtime without host/services")
	}
}

func TestModuleNagiosUnableToLocateCmdfile(t *testing.T) {
	conn := local()
	res, err := moduleNagios(context.Background(), conn, map[string]any{"action": "silence_nagios"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when no nagios.cfg can be found and no cmdfile was given")
	}
}
