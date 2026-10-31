package main

import (
	"bufio"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func listenSyslog(t *testing.T) (string, *net.UnixConn) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "log")
	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return path, conn
}

func readSyslog(t *testing.T, conn *net.UnixConn) string {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(buf[:n])
}

func assertNoSyslog(t *testing.T, conn *net.UnixConn) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	if n, err := conn.Read(make([]byte, 4096)); err == nil {
		t.Fatalf("unexpected line after %d bytes", n)
	}
}

func TestSyslogGetsWarningsAndErrorsOnly(t *testing.T) {
	path, conn := listenSyslog(t)
	logger := zerolog.New(newSyslogWriter(path)).With().Timestamp().Logger()

	logger.Debug().Msg("debug line")
	logger.Info().Str("address", "192.0.2.1").Msg("added to routing")
	assertNoSyslog(t, conn)

	logger.Warn().Err(errors.New("boom")).Msg("failed to clear stale ipsets")
	line := readSyslog(t, conn)
	if !strings.HasPrefix(line, "<28>") || !strings.Contains(line, " weirdtrickle[") ||
		!strings.HasSuffix(line, "]: WRN failed to clear stale ipsets error=boom\n") {
		t.Fatalf("warning line: %q", line)
	}

	logger.Error().Msg("failed to start application")
	if line := readSyslog(t, conn); !strings.HasPrefix(line, "<27>") || !strings.HasSuffix(line, ": ERR failed to start application\n") {
		t.Fatalf("error line: %q", line)
	}

	logger.WithLevel(zerolog.FatalLevel).Msg("failed to create PID file")
	if line := readSyslog(t, conn); !strings.HasPrefix(line, "<26>") {
		t.Fatalf("fatal line: %q", line)
	}
}

func TestSyslogCapsLinesPerMinute(t *testing.T) {
	path, conn := listenSyslog(t)
	writer := newSyslogWriter(path)
	logger := zerolog.New(writer)

	for range syslogPerMinute + 5 {
		logger.Error().Msg("failed to process request")
	}
	for range syslogPerMinute {
		readSyslog(t, conn)
	}
	assertNoSyslog(t, conn)

	// A startup failure still gets through, as it is the last thing the daemon says.
	logger.WithLevel(zerolog.FatalLevel).Msg("failed to create PID file")
	if line := readSyslog(t, conn); !strings.Contains(line, "failed to create PID file") {
		t.Fatalf("fatal line: %q", line)
	}

	writer.windowStart = time.Now().Add(-2 * time.Minute)
	logger.Warn().Msg("next minute")
	if line := readSyslog(t, conn); !strings.Contains(line, "5 messages dropped") {
		t.Fatalf("summary line: %q", line)
	}
	if line := readSyslog(t, conn); !strings.Contains(line, "next minute") {
		t.Fatalf("line after the window: %q", line)
	}
}

// Nothing reads the socket, so its queue fills up and a send would block forever
// without a deadline. The failed send then pauses the rest.
func TestSyslogSurvivesAStuckLogDaemon(t *testing.T) {
	path, _ := listenSyslog(t)
	writer := newSyslogWriter(path)

	start := time.Now()
	for range 5000 {
		writer.send(time.Now(), syslogError, strings.Repeat("x", 1024))
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("sends took %v", elapsed)
	}
	if writer.conn != nil || !time.Now().Before(writer.pausedUntil) {
		t.Fatal("a failed send must drop the connection and pause")
	}
}

func TestSyslogWithoutASocketIsHarmless(t *testing.T) {
	writer := newSyslogWriter(filepath.Join(t.TempDir(), "missing"))
	logger := zerolog.New(writer)

	logger.Error().Msg("failed to process request")
	if !time.Now().Before(writer.pausedUntil) {
		t.Fatal("a missing socket must pause further attempts")
	}
}

func TestSyslogFallsBackToAStreamSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	logger := zerolog.New(newSyslogWriter(path))
	logger.Warn().Msg("over a stream")

	_ = listener.(*net.UnixListener).SetDeadline(time.Now().Add(time.Second))
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || !strings.Contains(line, ": WRN over a stream") {
		t.Fatalf("line %q, err %v", line, err)
	}
}
