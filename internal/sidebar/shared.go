package sidebar

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
	"github.com/muesli/termenv"
	"tmux-agent/internal/tmux"
)

// Each tmux pane must retain a foreground process, but only this small relay
// runs there. The shared process owns every Bubble Tea model and worker.
type sharedHello struct {
	Pane string
	Env  []string
}
type lockedWriter struct {
	sync.Mutex
	w io.Writer
}

func (w *lockedWriter) Write(b []byte) (int, error) { w.Lock(); defer w.Unlock(); return w.w.Write(b) }

func sharedSocket() string {
	socket, _, _ := strings.Cut(os.Getenv("TMUX"), ",")
	exe, _ := os.Executable()
	st, _ := os.Stat(exe)
	version := ""
	if st != nil {
		version = fmt.Sprintf("%d-%d", st.Size(), st.ModTime().UnixNano())
	}
	sum := sha256.Sum256([]byte(socket + "\x00" + version))
	return filepath.Join(llmCacheDir(), fmt.Sprintf("sidebar-%x.sock", sum[:8]))
}

func dialShared() (net.Conn, error) {
	path := sharedSocket()
	if c, err := net.DialTimeout("unix", path, 200*time.Millisecond); err == nil {
		return c, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, "sidebar-server")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go cmd.Wait()
	var last error
	for i := 0; i < 100; i++ {
		if c, err := net.DialTimeout("unix", path, 200*time.Millisecond); err == nil {
			return c, nil
		} else {
			last = err
		}
		time.Sleep(30 * time.Millisecond)
	}
	return nil, last
}

// frameWriter serializes terminal bytes and control messages on one connection.
type frameWriter struct{ lockedWriter }

type sharedPrograms struct {
	sync.Mutex
	programs map[*tea.Program]struct{}
}

func (s *sharedPrograms) Send(msg tea.Msg) {
	s.Lock()
	programs := make([]*tea.Program, 0, len(s.programs))
	for p := range s.programs {
		programs = append(programs, p)
	}
	s.Unlock()
	for _, p := range programs {
		p.Send(msg)
	}
}

func (w *frameWriter) send(kind byte, data []byte) error {
	frame := make([]byte, 5+len(data))
	frame[0] = kind
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(data)))
	copy(frame[5:], data)
	_, err := w.lockedWriter.Write(frame)
	return err
}
func (w *frameWriter) Write(b []byte) (int, error) {
	if err := w.send('o', b); err != nil {
		return 0, err
	}
	return len(b), nil
}
func readFrame(r io.Reader) (byte, []byte, error) {
	var head [5]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(head[1:])
	if n > 4<<20 {
		return 0, nil, fmt.Errorf("frame too large")
	}
	b := make([]byte, n)
	_, err := io.ReadFull(r, b)
	return head[0], b, err
}

// RunSharedRelay owns only the terminal; the shared server owns the TUI.
func RunSharedRelay() int {
	pane := os.Getenv("TMUX_PANE")
	if pane == "" || os.Getenv("TMUX") == "" || !term.IsTerminal(os.Stdin.Fd()) {
		return Run()
	}
	state, err := term.MakeRaw(os.Stdin.Fd())
	if err != nil {
		return 1
	}
	defer term.Restore(os.Stdin.Fd(), state)
	tmux.SetPaneOption(pane, tmux.SidebarPID, strconv.Itoa(os.Getpid()))
	inputs := make(chan []byte, 16)
	go func() {
		defer close(inputs)
		b := make([]byte, 4096)
		for {
			n, e := os.Stdin.Read(b)
			if n > 0 {
				inputs <- append([]byte(nil), b[:n]...)
			}
			if e != nil {
				return
			}
		}
	}()
	sig := make(chan os.Signal, 8)
	signal.Notify(sig, syscall.SIGWINCH, syscall.SIGUSR1)
	defer signal.Stop(sig)
	for {
		conn, err := dialShared()
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		var env []string
		for _, kv := range os.Environ() {
			if strings.HasPrefix(kv, "TERM=") || strings.HasPrefix(kv, "COLORTERM=") || strings.HasPrefix(kv, "NO_COLOR=") {
				env = append(env, kv)
			}
		}
		if json.NewEncoder(conn).Encode(sharedHello{Pane: pane, Env: env}) != nil {
			conn.Close()
			continue
		}
		writer := &frameWriter{lockedWriter: lockedWriter{w: conn}}
		resize := func() {
			w, h, err := term.GetSize(os.Stdout.Fd())
			if err == nil {
				b := make([]byte, 8)
				binary.BigEndian.PutUint32(b[:4], uint32(w))
				binary.BigEndian.PutUint32(b[4:], uint32(h))
				_ = writer.send('s', b)
			}
		}
		done := make(chan struct{})
		go func() {
			defer conn.Close()
			for {
				select {
				case b, ok := <-inputs:
					if !ok {
						return
					}
					if writer.send('i', b) != nil {
						return
					}
				case s := <-sig:
					if s == syscall.SIGWINCH {
						resize()
					} else {
						_ = writer.send('r', nil)
					}
				case <-done:
					return
				}
			}
		}()
		resize()
		quit := false
		for {
			kind, b, err := readFrame(conn)
			if err != nil {
				break
			}
			if kind == 'q' {
				quit = true
				break
			}
			if kind == 'o' {
				if _, err = os.Stdout.Write(b); err != nil {
					quit = true
					break
				}
			}
		}
		close(done)
		conn.Close()
		if quit {
			return 0
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// SharedServer runs one server per tmux socket and binary version.
func SharedServer() int {
	// The first relay's pane may disappear while other sidebars remain.
	// Per-pane operations use their explicit pane ID instead.
	_ = os.Unsetenv("TMUX_PANE")
	// The daemon has no terminal on stdout; its renderers write to pane relays.
	if os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb" {
		profile := termenv.ANSI256
		if strings.Contains(strings.ToLower(os.Getenv("COLORTERM")), "truecolor") || strings.Contains(strings.ToLower(os.Getenv("COLORTERM")), "24bit") {
			profile = termenv.TrueColor
		}
		lipgloss.SetColorProfile(profile)
	}
	path := sharedSocket()
	if os.MkdirAll(filepath.Dir(path), 0700) != nil {
		return 1
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return 1
	}
	defer lock.Close()
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return 0
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	_ = os.Remove(path)
	listener, err := net.Listen("unix", path)
	if err != nil {
		return 1
	}
	defer listener.Close()
	defer os.Remove(path)
	_ = os.Chmod(path, 0600)
	programs := &sharedPrograms{programs: make(map[*tea.Program]struct{})}
	done := make(chan struct{})
	defer close(done)
	startBackgroundWorkers(programs, done)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				programs.Lock()
				empty := len(programs.programs) == 0
				programs.Unlock()
				if empty {
					listener.Close()
					return
				}
			case <-done:
				return
			}
		}
	}()
	for {
		c, err := listener.Accept()
		if err != nil {
			return 0
		}
		go serveSidebar(c, programs)
	}
}

func serveSidebar(c net.Conn, programs *sharedPrograms) {
	defer c.Close()
	// Keep buffered terminal frames after the header for the frame reader.
	r := bufio.NewReader(c)
	line, err := r.ReadSlice('\n')
	if err != nil {
		return
	}
	var hello sharedHello
	if json.Unmarshal(line, &hello) != nil || !strings.HasPrefix(hello.Pane, "%") {
		return
	}
	if _, err := strconv.Atoi(strings.TrimPrefix(hello.Pane, "%")); err != nil {
		return
	}
	in, out := io.Pipe()
	defer in.Close()
	defer out.Close()
	writer := &frameWriter{lockedWriter: lockedWriter{w: c}}
	m := NewModel(hello.Pane)
	m.output = writer
	m.windowID = tmux.DisplayMessage(hello.Pane, "#{window_id}")
	p := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(writer), tea.WithEnvironment(hello.Env), tea.WithoutSignalHandler(), tea.WithMouseCellMotion())
	tmux.SetPaneOption(hello.Pane, "@sidebar_server_pid", strconv.Itoa(os.Getpid()))
	programs.Lock()
	programs.programs[p] = struct{}{}
	programs.Unlock()
	defer func() { programs.Lock(); delete(programs.programs, p); programs.Unlock() }()
	go func() {
		defer p.Quit()
		for {
			var head [5]byte
			if _, err := io.ReadFull(r, head[:]); err != nil {
				return
			}
			n := binary.BigEndian.Uint32(head[1:])
			if n > 65536 {
				return
			}
			b := make([]byte, n)
			if _, err := io.ReadFull(r, b); err != nil {
				return
			}
			switch head[0] {
			case 'i':
				if _, err := out.Write(b); err != nil {
					return
				}
			case 's':
				if len(b) == 8 {
					p.Send(tea.WindowSizeMsg{Width: int(binary.BigEndian.Uint32(b[:4])), Height: int(binary.BigEndian.Uint32(b[4:]))})
				}
			case 'r':
				p.Send(refreshRequestMsg{})
			}
		}
	}()
	_, _ = p.Run()
	_ = writer.send('q', nil)
}
