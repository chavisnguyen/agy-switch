package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ptyMenuScript drives `agys` (the account picker) in a real pseudo-terminal, the way a user's
// terminal would, and reports what happened for each scenario as JSON on its last line.
const ptyMenuScript = `
import os, pty, sys, time, select, json, re, signal

binary, home, bindir = sys.argv[1], sys.argv[2], sys.argv[3]
ANSI = re.compile(r"\x1b\[[0-9;?]*[A-Za-z]")

def drain(fd, t):
    out, end = b"", time.time() + t
    while time.time() < end:
        r, _, _ = select.select([fd], [], [], 0.05)
        if r:
            try:
                d = os.read(fd, 65536)
            except OSError:
                break
            if not d:
                break
            out += d
    return out

def start():
    pid, fd = pty.fork()
    if pid == 0:
        os.environ.update(HOME=home, TERM="xterm-256color", PATH=bindir + ":/usr/bin:/bin")
        os.execv(binary, [binary])
    out, deadline = b"", time.time() + 20
    while time.time() < deadline and b"q/Esc: cancel" not in out:
        out += drain(fd, 0.3)
    time.sleep(0.3)
    out += drain(fd, 0.3)
    return pid, fd, out

def finish(pid, fd, wait=3.0):
    tail, end = b"", time.time() + wait
    exited = False
    while time.time() < end:
        tail += drain(fd, 0.1)
        try:
            p, _ = os.waitpid(pid, os.WNOHANG)
        except ChildProcessError:
            exited = True
            break
        if p:
            exited = True
            break
    if not exited:
        try:
            os.kill(pid, signal.SIGKILL)
            os.waitpid(pid, 0)
        except Exception:
            pass
    return tail, exited

def last_row(data):
    rows = re.findall(r"❯ [^\n]*?(acc\d)", ANSI.sub("", data.decode("utf-8", "replace")))
    return rows[-1] if rows else None

def scenario(sends, final=None, wait=3.0):
    pid, fd, _ = start()
    seen = b""
    for i, s in enumerate(sends):
        if isinstance(s, float):
            time.sleep(s)
            continue
        os.write(fd, s)
        # do not wait for output between the two halves of a split sequence
        if i + 1 < len(sends) and isinstance(sends[i + 1], float):
            continue
        seen += drain(fd, 0.4)
    during = seen
    if final is not None:
        os.write(fd, final)
    tail, exited = finish(pid, fd, wait)
    text = (during + tail).decode("utf-8", "replace")
    return {
        "echoed": during.count(b"^[["),
        "redraws": during.count(b"SELECT AGY ACCOUNT"),
        "row": last_row(during),
        "exited": exited,
        "exited_msg": "Exited" in text,
        "switched_msg": "Switched" in text or "switched" in text,
    }

res = {}
res["arrows"] = scenario([b"\x1b[B", b"\x1b[B", b"\x1b[A", b"\x1b[B"], final=b"q")
res["coalesced"] = scenario([b"\x1b[B\x1b[B"], final=b"q")
res["split"] = scenario([b"\x1b", 0.03, b"[B"], final=b"q")
res["esc"] = scenario([], final=b"\x1b")
res["vim"] = scenario([b"j", b"j", b"k"], final=b"q")
res["enter"] = scenario([b"\x1b[B"], final=b"\r", wait=8.0)
res["current"] = open(os.path.join(home, ".gemini", "antigravity-cli", "profiles", ".current")).read().strip()
print("RESULT " + json.dumps(res))
`

type ptyScenario struct {
	Echoed    int    `json:"echoed"`
	Redraws   int    `json:"redraws"`
	Row       string `json:"row"`
	Exited    bool   `json:"exited"`
	ExitedMsg bool   `json:"exited_msg"`
}

// TestE2E_InteractiveMenu_Pty runs the account picker in a real pseudo-terminal. Regression test for
// arrow keys echoing as "^[[B" because raw mode was never enabled.
func TestE2E_InteractiveMenu_Pty(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pty test needs a unix system")
	}
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}

	h := NewHarness(t)
	for _, n := range []string{"acc1", "acc2", "acc3"} {
		if err := h.CreateFakeGoogleAccount(n, n+"@gmail.com", n, "s"); err != nil {
			t.Fatalf("Failed to create profile: %v", err)
		}
	}
	_ = h.SetActiveAccount("acc1")

	script := filepath.Join(h.TempDir, "pty_menu.py")
	if err := os.WriteFile(script, []byte(ptyMenuScript), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(py, "-I", script, h.AgysBin, h.TempDir, h.BinDir)
	done := make(chan struct{})
	var out []byte
	go func() { out, err = cmd.CombinedOutput(); close(done) }()
	select {
	case <-done:
	case <-time.After(150 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("pty driver timed out")
	}
	if err != nil {
		t.Fatalf("pty driver failed: %v\n%s", err, out)
	}

	var line string
	for _, l := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(l, "RESULT ") {
			line = strings.TrimPrefix(l, "RESULT ")
		}
	}
	if line == "" {
		t.Fatalf("no RESULT from pty driver:\n%s", out)
	}
	var res map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &res); err != nil {
		t.Fatalf("bad RESULT json: %v\n%s", err, line)
	}
	get := func(name string) ptyScenario {
		var s ptyScenario
		if err := json.Unmarshal(res[name], &s); err != nil {
			t.Fatalf("bad scenario %s: %v", name, err)
		}
		return s
	}

	check := func(name string, wantRedraws int, wantRow string) {
		s := get(name)
		if s.Echoed != 0 {
			t.Errorf("%s: keys were echoed to the terminal (%d times) - raw mode is not active", name, s.Echoed)
		}
		if s.Redraws != wantRedraws {
			t.Errorf("%s: redraws = %d, want %d", name, s.Redraws, wantRedraws)
		}
		if s.Row != wantRow {
			t.Errorf("%s: selected row = %q, want %q", name, s.Row, wantRow)
		}
		if !s.Exited || !s.ExitedMsg {
			t.Errorf("%s: menu did not exit cleanly on cancel (exited=%v msg=%v)", name, s.Exited, s.ExitedMsg)
		}
	}
	// start on acc1 (active)
	check("arrows", 4, "acc3")    // down, down, up, down
	check("coalesced", 1, "acc3") // two keys in one read -> one redraw
	check("split", 1, "acc2")     // ESC and "[B" arrive separately -> still a Down, not a cancel
	check("vim", 3, "acc2")       // j, j, k

	esc := get("esc")
	if !esc.Exited || !esc.ExitedMsg {
		t.Errorf("lone Esc should cancel the menu (exited=%v msg=%v)", esc.Exited, esc.ExitedMsg)
	}

	var current string
	_ = json.Unmarshal(res["current"], &current)
	if current != "acc2" {
		t.Errorf("Down then Enter should switch to acc2, active profile is %q", current)
	}
}
