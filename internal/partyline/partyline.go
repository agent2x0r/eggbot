// Package partyline is the telnet/TLS/DCC admin console (. commands, owner chat).
// Default listen address should stay loopback unless TLS is configured.
package partyline

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"
)

type Session struct {
	Conn    net.Conn
	Handle  string
	Console string // eggdrop-style letters: m j p k n t o
	ChatOn  bool
	Remote  string
	Kind    string // telnet, dcc, tls

	wmu sync.Mutex
	out chan string
}

func (s *Session) Write(line string) {
	if s.out == nil {
		s.writeDirect(line)
		return
	}
	select {
	case s.out <- line:
	default:
		if s.Conn != nil {
			_ = s.Conn.Close()
		}
	}
}

func (s *Session) writeDirect(line string) {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if s.Conn == nil {
		return
	}
	_ = s.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, _ = io.WriteString(s.Conn, line+"\r\n")
}

func (s *Session) writeLoop() {
	for line := range s.out {
		s.writeDirect(line)
	}
}

func (s *Session) Printf(format string, args ...any) {
	s.Write(fmt.Sprintf(format, args...))
}

type Server struct {
	Listen    string
	TLSListen string
	TLSCert   string
	TLSKey    string
	Log       *slog.Logger

	Authenticate func(handle, password string) error
	OnLine       func(s *Session, line string)
	OnJoin       func(s *Session)
	OnLeave      func(s *Session)

	mu       sync.Mutex
	sessions []*Session
	ln       net.Listener
	tlsLn    net.Listener
	stop     chan struct{}
	slots    chan struct{}

	fails   map[string]failRec
	failsMu sync.Mutex
}

type failRec struct {
	n     int
	until time.Time
	last  time.Time
}

const (
	maxPartyConnections = 64
	maxHandleBytes      = 64
	maxPasswordBytes    = 1024
	maxCommandBytes     = 4096
	maxFailRecords      = 4096
)

func (s *Server) Start() error {
	s.stop = make(chan struct{})
	s.fails = map[string]failRec{}
	s.slots = make(chan struct{}, maxPartyConnections)
	if s.Listen != "" {
		ln, err := net.Listen("tcp", s.Listen)
		if err != nil {
			return fmt.Errorf("partyline listen %s: %w", s.Listen, err)
		}
		s.ln = ln
		s.Log.Info("partyline listening", "addr", s.Listen)
		go s.accept(ln, "telnet")
	}
	if s.TLSListen != "" && s.TLSCert != "" && s.TLSKey != "" {
		cert, err := tls.LoadX509KeyPair(s.TLSCert, s.TLSKey)
		if err != nil {
			return fmt.Errorf("partyline tls cert: %w", err)
		}
		ln, err := tls.Listen("tcp", s.TLSListen, &tls.Config{Certificates: []tls.Certificate{cert}})
		if err != nil {
			return fmt.Errorf("partyline tls listen: %w", err)
		}
		s.tlsLn = ln
		s.Log.Info("partyline tls listening", "addr", s.TLSListen)
		go s.accept(ln, "tls")
	}
	return nil
}

func (s *Server) Stop() {
	if s.stop != nil {
		select {
		case <-s.stop:
		default:
			close(s.stop)
		}
	}
	if s.ln != nil {
		s.ln.Close()
	}
	if s.tlsLn != nil {
		s.tlsLn.Close()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sess := range s.sessions {
		sess.Conn.Close()
	}
}

func (s *Server) accept(ln net.Listener, kind string) {
	for {
		c, err := ln.Accept()
		if err != nil {
			select {
			case <-s.stop:
				return
			default:
				s.Log.Debug("partyline accept", "err", err)
				return
			}
		}
		if !s.claimConn() {
			_, _ = io.WriteString(c, "partyline busy\r\n")
			_ = c.Close()
			continue
		}
		go s.serveClaimed(c, kind)
	}
}

// ServeConn is used for DCC CHAT as well as telnet.
func (s *Server) ServeConn(c net.Conn, kind string) {
	if !s.claimConn() {
		_ = c.Close()
		return
	}
	go s.serveClaimed(c, kind)
}

func (s *Server) serveClaimed(c net.Conn, kind string) {
	defer s.releaseConn()
	s.serve(c, kind)
}

func (s *Server) claimConn() bool {
	s.mu.Lock()
	if s.slots == nil {
		s.slots = make(chan struct{}, maxPartyConnections)
	}
	slots := s.slots
	s.mu.Unlock()
	select {
	case slots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Server) releaseConn() {
	s.mu.Lock()
	slots := s.slots
	s.mu.Unlock()
	if slots != nil {
		<-slots
	}
}

func (s *Server) serve(c net.Conn, kind string) {
	defer c.Close()
	remote := c.RemoteAddr().String()
	if s.blocked(remote) {
		_, _ = io.WriteString(c, "too many failed logins\r\n")
		return
	}
	_ = c.SetDeadline(time.Now().Add(60 * time.Second))
	r := bufio.NewReader(c)
	_, _ = io.WriteString(c, "\r\n      eggbot partyline\r\n\r\n")
	_, _ = io.WriteString(c, "handle: ")
	handle, err := readLine(r, c, maxHandleBytes)
	if err != nil {
		return
	}
	_, _ = io.WriteString(c, "password: ")
	pw, err := readLine(r, c, maxPasswordBytes)
	if err != nil {
		return
	}
	_ = c.SetDeadline(time.Time{})
	handle = strings.TrimSpace(handle)
	if s.Authenticate == nil {
		_, _ = io.WriteString(c, "login failed\r\n")
		return
	}
	if err := s.Authenticate(handle, pw); err != nil {
		s.recordFail(remote)
		if s.Log != nil {
			s.Log.Info("partyline login failed", "remote", remote, "err", err)
		}
		_, _ = io.WriteString(c, "login failed\r\n")
		return
	}
	s.clearFail(remote)
	sess := &Session{
		Conn:    c,
		Handle:  handle,
		Console: "jpk",
		ChatOn:  true,
		Remote:  remote,
		Kind:    kind,
		out:     make(chan string, 64),
	}
	go sess.writeLoop()
	s.add(sess)
	defer func() {
		s.remove(sess)
		close(sess.out)
	}()
	sess.Write("welcome to the partyline, " + handle + ". type .help")
	if s.OnJoin != nil {
		s.OnJoin(sess)
	}
	if s.OnLeave != nil {
		defer s.OnLeave(sess)
	}
	for {
		_ = c.SetReadDeadline(time.Now().Add(30 * time.Minute))
		line, err := readLine(r, c, maxCommandBytes)
		if err != nil {
			return
		}
		if s.OnLine != nil {
			s.OnLine(sess, line)
		}
	}
}

func (s *Server) add(sess *Session) {
	s.mu.Lock()
	s.sessions = append(s.sessions, sess)
	s.mu.Unlock()
}

func (s *Server) remove(sess *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.sessions[:0]
	for _, x := range s.sessions {
		if x != sess {
			out = append(out, x)
		}
	}
	s.sessions = out
}

func (s *Server) Sessions() []*Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Session, len(s.sessions))
	copy(out, s.sessions)
	return out
}

func (s *Server) Broadcast(text string, ownersOnly bool, ownerCheck func(handle string) bool) {
	for _, sess := range s.Sessions() {
		if ownersOnly && ownerCheck != nil && !ownerCheck(sess.Handle) {
			continue
		}
		if !sess.ChatOn {
			continue
		}
		sess.Write(text)
	}
}

func (s *Server) Consolef(mode byte, format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	for _, sess := range s.Sessions() {
		if strings.IndexByte(sess.Console, mode) >= 0 {
			sess.Write(line)
		}
	}
}

func (s *Server) blocked(remote string) bool {
	host := remoteHost(remote)
	now := time.Now()
	s.failsMu.Lock()
	defer s.failsMu.Unlock()
	if s.fails == nil {
		s.fails = make(map[string]failRec)
	}
	s.pruneFailuresLocked(now)
	f := s.fails[host]
	if !f.until.IsZero() && now.Before(f.until) {
		return true
	}
	_, known := s.fails[host]
	return !known && len(s.fails) >= maxFailRecords
}

func (s *Server) recordFail(remote string) {
	host := remoteHost(remote)
	now := time.Now()
	s.failsMu.Lock()
	defer s.failsMu.Unlock()
	if s.fails == nil {
		s.fails = make(map[string]failRec)
	}
	s.pruneFailuresLocked(now)
	if _, known := s.fails[host]; !known && len(s.fails) >= maxFailRecords {
		return
	}
	f := s.fails[host]
	f.n++
	f.last = now
	if f.n >= 3 {
		f.until = now.Add(10 * time.Minute)
		f.n = 0
	}
	s.fails[host] = f
}

func (s *Server) clearFail(remote string) {
	host := remoteHost(remote)
	s.failsMu.Lock()
	delete(s.fails, host)
	s.failsMu.Unlock()
}

func (s *Server) pruneFailuresLocked(now time.Time) {
	for host, rec := range s.fails {
		if !rec.until.IsZero() && now.Before(rec.until) {
			continue
		}
		if now.Sub(rec.last) >= 10*time.Minute {
			delete(s.fails, host)
		}
	}
}

func remoteHost(remote string) string {
	host, _, err := net.SplitHostPort(remote)
	if err != nil || host == "" {
		return remote
	}
	return host
}
