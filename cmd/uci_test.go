package main

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bluescreen10/chester"
)

func TestParseGoOptions(t *testing.T) {
	pos, err := chester.ParseFEN(chester.DefaultFEN)
	if err != nil {
		t.Fatalf("ParseFEN: %v", err)
	}

	server := &UCIServer{
		pos:          pos,
		moveOverhead: 10 * time.Millisecond,
	}

	t.Run("movetime", func(t *testing.T) {
		options := server.parseGoOptions([]string{"movetime", "50"})
		if options.softLimit != 50*time.Millisecond || options.hardLimit != 50*time.Millisecond {
			t.Fatalf("unexpected movetime limits: %#v", options)
		}
	})

	t.Run("clockWithIncrement", func(t *testing.T) {
		options := server.parseGoOptions([]string{"wtime", "1000", "btime", "1000", "winc", "100", "binc", "100"})
		if options.softLimit != 80*time.Millisecond {
			t.Fatalf("unexpected soft limit: %s", options.softLimit)
		}
		if options.hardLimit != 120*time.Millisecond {
			t.Fatalf("unexpected hard limit: %s", options.hardLimit)
		}
	})

	t.Run("movestogo", func(t *testing.T) {
		options := server.parseGoOptions([]string{"wtime", "1000", "btime", "1000", "movestogo", "10"})
		if options.softLimit != 82*time.Millisecond {
			t.Fatalf("unexpected soft limit: %s", options.softLimit)
		}
		if options.hardLimit != 123*time.Millisecond {
			t.Fatalf("unexpected hard limit: %s", options.hardLimit)
		}
	})

	t.Run("depth", func(t *testing.T) {
		options := server.parseGoOptions([]string{"depth", "6"})
		if options.depth != 6 || options.infinite {
			t.Fatalf("unexpected depth options: %#v", options)
		}
	})

	t.Run("infinite", func(t *testing.T) {
		options := server.parseGoOptions([]string{"infinite"})
		if !options.infinite || options.softLimit != 0 || options.hardLimit != 0 {
			t.Fatalf("unexpected infinite options: %#v", options)
		}
	})
}

func TestUCIHandshake(t *testing.T) {
	session := newUCITestSession(t)
	defer session.close()

	session.waitForLine("uciok", 5*time.Second)
	session.send("isready")
	session.waitForLine("readyok", 5*time.Second)
}

func TestUCIDepth6FromStartpos(t *testing.T) {
	session := newUCITestSession(t)
	defer session.close()

	session.waitForLine("uciok", 5*time.Second)
	session.send("position startpos")

	start := time.Now()
	session.send("go depth 6")
	bestmove := session.waitForPrefix("bestmove ", 30*time.Second)
	if bestmove == "bestmove 0000" {
		t.Fatal("expected a legal bestmove at depth 6")
	}

	if time.Since(start) > 30*time.Second {
		t.Fatal("depth 6 search exceeded timeout")
	}
}

func TestUCIMovetime50(t *testing.T) {
	session := newUCITestSession(t)
	defer session.close()

	session.waitForLine("uciok", 5*time.Second)
	session.send("position startpos")

	start := time.Now()
	session.send("go movetime 50")
	bestmove := session.waitForPrefix("bestmove ", 5*time.Second)
	if bestmove == "bestmove 0000" {
		t.Fatal("expected a legal bestmove for movetime search")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("movetime search exceeded expected bound")
	}
	if session.countPrefix("bestmove ") != 1 {
		t.Fatalf("expected exactly one bestmove, got %d", session.countPrefix("bestmove "))
	}
}

func TestUCIInfiniteStop(t *testing.T) {
	session := newUCITestSession(t)
	defer session.close()

	session.waitForLine("uciok", 5*time.Second)
	session.send("position startpos")
	session.send("go infinite")
	time.Sleep(100 * time.Millisecond)
	session.send("stop")

	bestmove := session.waitForPrefix("bestmove ", 5*time.Second)
	if bestmove == "bestmove 0000" {
		t.Fatal("expected a legal bestmove for infinite search")
	}
	if session.countPrefix("bestmove ") != 1 {
		t.Fatalf("expected exactly one bestmove, got %d", session.countPrefix("bestmove "))
	}
}

type uciTestSession struct {
	t      *testing.T
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  chan string
	buffer []string
}

func newUCITestSession(t *testing.T) *uciTestSession {
	t.Helper()

	cmd := exec.Command(testBinaryPath(t))
	cmd.Dir = filepath.Dir(testBinaryPath(t))

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	session := &uciTestSession{
		t:     t,
		cmd:   cmd,
		stdin: stdin,
		lines: make(chan string, 256),
	}

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			session.lines <- scanner.Text()
		}
		close(session.lines)
	}()

	session.send("uci")
	return session
}

func (s *uciTestSession) send(command string) {
	s.t.Helper()
	if _, err := s.stdin.Write([]byte(command + "\n")); err != nil {
		s.t.Fatalf("send %q: %v", command, err)
	}
}

func (s *uciTestSession) waitForLine(expected string, timeout time.Duration) string {
	s.t.Helper()
	return s.waitFor(func(line string) bool { return line == expected }, timeout)
}

func (s *uciTestSession) waitForPrefix(prefix string, timeout time.Duration) string {
	s.t.Helper()
	return s.waitFor(func(line string) bool { return strings.HasPrefix(line, prefix) }, timeout)
}

func (s *uciTestSession) waitFor(match func(string) bool, timeout time.Duration) string {
	s.t.Helper()

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	for {
		select {
		case line, ok := <-s.lines:
			if !ok {
				s.t.Fatal("engine exited before expected output")
			}
			s.buffer = append(s.buffer, line)
			if match(line) {
				return line
			}
		case <-deadline.C:
			s.t.Fatalf("timed out waiting for output; got %v", s.buffer)
		}
	}
}

func (s *uciTestSession) countPrefix(prefix string) int {
	count := 0
	for _, line := range s.buffer {
		if strings.HasPrefix(line, prefix) {
			count++
		}
	}
	return count
}

func (s *uciTestSession) close() {
	if s.stdin != nil {
		_, _ = s.stdin.Write([]byte("quit\n"))
		_ = s.stdin.Close()
	}
	_ = s.cmd.Wait()
}

var (
	buildOnce sync.Once
	buildErr  error
	cachedBin string
)

func testBinaryPath(t *testing.T) string {
	t.Helper()

	buildOnce.Do(func() {
		tempDir, err := os.MkdirTemp("", "chester-cmd-test-*")
		if err != nil {
			buildErr = err
			return
		}
		cachedBin = filepath.Join(tempDir, "chester-test.exe")

		cmd := exec.Command("go", "build", "-o", cachedBin, ".")
		cmd.Dir = "."
		buildErr = cmd.Run()
	})

	if buildErr != nil {
		t.Fatalf("failed to build test binary: %v", buildErr)
	}

	return cachedBin
}
