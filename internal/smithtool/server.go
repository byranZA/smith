package smithtool

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// socketSuffix ends every socket's file name.
const socketSuffix = ".sock"

// randomNameBytes is how many random bytes name a socket.
const randomNameBytes = 8

// pathLimit is the longest socket path the platform allows, with its NUL.
func pathLimit() int {
	if runtime.GOOS == "darwin" {
		return 104
	}
	return 108
}

// TooLongError is a tools directory whose sockets' paths would exceed the platform's limit.
type TooLongError struct {
	Dir   string
	Limit int
}

// Error says the directory is too long for the tool socket.
func (e *TooLongError) Error() string {
	return fmt.Sprintf("the state home's path is too long for the tool socket: %s would put sockets past the platform's %d-byte limit", e.Dir, e.Limit-1)
}

// PrepareDir makes the tools directory dir, mode 0700, and clears the stale
// sockets in it: those nothing answers on. It refuses a dir whose sockets'
// paths would exceed the platform limit, before making anything.
func PrepareDir(dir string) error {
	longest := filepath.Join(dir, strings.Repeat("x", randomNameBytes*2)+socketSuffix)
	if len(longest) >= pathLimit() {
		return &TooLongError{Dir: dir, Limit: pathLimit()}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("make the tools directory %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("restrict the tools directory %s: %w", dir, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read the tools directory %s: %w", dir, err)
	}
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if !strings.HasSuffix(e.Name(), socketSuffix) || alive(path) {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("clear the stale socket %s: %w", path, err)
		}
	}
	return nil
}

// alive reports whether something answers on the socket at path.
func alive(path string) bool {
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// Server serves one registry on one socket, one call at a time.
type Server struct {
	path     string
	listener net.Listener
	registry Registry
	onCall   func(Served)
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	turn     sync.Mutex
}

// Serve starts a server for registry on a new socket with a random name in
// dir, which PrepareDir has prepared. onCall, when set, is told of each call
// once it ends.
func Serve(dir string, registry Registry, onCall func(Served)) (*Server, error) {
	var raw [randomNameBytes]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, fmt.Errorf("name the tool socket: %w", err)
	}
	path := filepath.Join(dir, hex.EncodeToString(raw[:])+socketSuffix)
	if len(path) >= pathLimit() {
		return nil, &TooLongError{Dir: dir, Limit: pathLimit()}
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen on the tool socket %s: %w", path, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{path: path, listener: listener, registry: registry, onCall: onCall, ctx: ctx, cancel: cancel}
	s.wg.Add(1)
	go s.accept()
	return s, nil
}

// Path returns the socket's path, for SMITH_TOOLS.
func (s *Server) Path() string { return s.path }

// Close stops serving, cancels a running call, waits for it to end and
// removes the socket.
func (s *Server) Close() error {
	s.cancel()
	err := s.listener.Close()
	s.wg.Wait()
	if rerr := os.Remove(s.path); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
		err = errors.Join(err, fmt.Errorf("remove the tool socket %s: %w", s.path, rerr))
	}
	if err != nil && !errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("close the tool server: %w", err)
	}
	return nil
}

// accept serves each connection in its own goroutine until the listener closes.
func (s *Server) accept() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.serve(conn)
		}()
	}
}

// serve answers the one request on conn and closes it.
func (s *Server) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	stop := context.AfterFunc(s.ctx, func() { _ = conn.Close() })
	defer stop()
	var req request
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&req); err != nil {
		return
	}
	enc := json.NewEncoder(conn)
	switch req.Op {
	case opList:
		tools, err := json.Marshal(s.registry.List())
		if err != nil {
			_ = enc.Encode(frame{Done: true, Status: statusFailed, Error: err.Error()})
			return
		}
		_ = enc.Encode(frame{Done: true, Tools: tools})
	case opCall:
		s.call(conn, enc, req)
	default:
		_ = enc.Encode(frame{Done: true, Status: statusRefused, Error: fmt.Sprintf("unknown request %q", req.Op)})
	}
}

// call runs the tool req names, once the call before it is done, streaming
// its output and ending with its status.
func (s *Server) call(conn net.Conn, enc *json.Encoder, req request) {
	tool, ok := s.registry.find(req.Name)
	if !ok {
		err := s.registry.unknown(req.Name)
		if s.onCall != nil {
			s.onCall(Served{Name: req.Name, Err: err, Refused: true})
		}
		_ = enc.Encode(frame{Done: true, Status: statusRefused, Error: err.Error()})
		return
	}
	s.turn.Lock()
	defer s.turn.Unlock()
	if s.ctx.Err() != nil {
		return
	}
	began := time.Now()
	err := tool.Run(s.ctx, req.Args, frameWriter{enc: enc})
	end := frame{Done: true}
	var refused *RefusedError
	switch {
	case errors.As(err, &refused):
		end.Status, end.Error = statusRefused, err.Error()
	case err != nil:
		end.Status, end.Error = statusFailed, err.Error()
	}
	if s.onCall != nil {
		s.onCall(Served{Name: tool.Name, Err: err, Took: time.Since(began)})
	}
	_ = enc.Encode(end)
}

// frameWriter writes a tool's output as frames.
type frameWriter struct {
	enc *json.Encoder
}

// Write sends p as one output frame.
func (w frameWriter) Write(p []byte) (int, error) {
	if err := w.enc.Encode(frame{Out: p}); err != nil {
		return 0, fmt.Errorf("stream tool output: %w", err)
	}
	return len(p), nil
}

var _ io.Writer = frameWriter{}
