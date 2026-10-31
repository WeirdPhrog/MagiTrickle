package main

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

const (
	syslogPath      = "/dev/log"
	syslogTag       = "weirdtrickle"
	syslogTimeout   = 100 * time.Millisecond
	syslogRetry     = time.Minute
	syslogPerMinute = 30

	syslogDaemon   = 3 << 3
	syslogCritical = 2
	syslogError    = 3
	syslogWarning  = 4
)

// syslogWriter copies warnings and errors to the system log: the init script
// discards the daemon's output, so this is where they stay visible. Info stays
// out, as it logs a line per routed address.
//
// It runs on the DNS path, where a failure logs once per query. So a send has a
// deadline and a failed one pauses sending, as a stuck log daemon must cost lost
// lines rather than a stalled network, and a minute's lines are capped to keep
// the rest of the router's log.
type syslogWriter struct {
	path    string
	mu      sync.Mutex
	conn    net.Conn
	text    bytes.Buffer
	console zerolog.ConsoleWriter

	pausedUntil time.Time
	windowStart time.Time
	sent        int
	dropped     int
}

func newSyslogWriter(path string) *syslogWriter {
	s := &syslogWriter{path: path}
	s.console = zerolog.ConsoleWriter{Out: &s.text, NoColor: true, PartsExclude: []string{zerolog.TimestampFieldName}}
	return s
}

func syslogSeverity(level zerolog.Level) (int, bool) {
	switch level {
	case zerolog.WarnLevel:
		return syslogWarning, true
	case zerolog.ErrorLevel:
		return syslogError, true
	case zerolog.FatalLevel, zerolog.PanicLevel:
		return syslogCritical, true
	}
	return 0, false
}

func (s *syslogWriter) Write(p []byte) (int, error) {
	return s.WriteLevel(zerolog.NoLevel, p)
}

func (s *syslogWriter) WriteLevel(level zerolog.Level, p []byte) (int, error) {
	severity, ok := syslogSeverity(level)
	if !ok {
		return len(p), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if now.Sub(s.windowStart) >= time.Minute {
		if s.dropped > 0 {
			s.send(now, syslogWarning, fmt.Sprintf("WRN %d messages dropped: too many in a minute", s.dropped))
		}
		s.windowStart, s.sent, s.dropped = now, 0, 0
	}
	if s.sent >= syslogPerMinute && level < zerolog.FatalLevel {
		s.dropped++
		return len(p), nil
	}
	s.sent++

	s.text.Reset()
	if _, err := s.console.Write(p); err == nil {
		s.send(now, severity, strings.TrimSpace(s.text.String()))
	}
	return len(p), nil
}

func (s *syslogWriter) send(now time.Time, severity int, message string) {
	if now.Before(s.pausedUntil) {
		return
	}
	if s.conn == nil {
		if s.conn = dialSyslog(s.path); s.conn == nil {
			s.pausedUntil = now.Add(syslogRetry)
			return
		}
	}
	_ = s.conn.SetWriteDeadline(now.Add(syslogTimeout))
	line := fmt.Sprintf("<%d>%s %s[%d]: %s\n", syslogDaemon|severity, now.Format(time.Stamp), syslogTag, os.Getpid(), message)
	if _, err := s.conn.Write([]byte(line)); err != nil {
		_ = s.conn.Close()
		s.conn, s.pausedUntil = nil, now.Add(syslogRetry)
	}
}

func dialSyslog(path string) net.Conn {
	for _, network := range []string{"unixgram", "unix"} {
		if conn, err := net.DialTimeout(network, path, syslogTimeout); err == nil {
			return conn
		}
	}
	return nil
}
