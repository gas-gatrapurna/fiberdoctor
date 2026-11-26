// Package cli menyediakan klien CLI generik untuk perangkat jaringan yang
// diakses lewat Telnet.
//
// Klien ini diekstrak dari driver ZTE C320 yang sudah terverifikasi terhadap
// perangkat live (OLT-SYNTH-A & OLT-SYNTH-B, 2026-11-05) supaya
// ZTE dan C-Data memakai mesin yang sama:
//
//   - Pembacaan LINEAR (bukan scan ulang seluruh buffer per byte) sehingga
//     output besar seperti `show running-config` (437 KB) tidak timeout.
//   - Framing output memakai PROMPT PENUH, bukan karakter '#' tunggal, sehingga
//     '#' di dalam data tidak salah dipotong.
//   - Race-free: tidak ada buffer yang diakses dua goroutine tanpa lock.
package cli

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	hostMutexMap sync.Map
)

// HostMutex mengembalikan mutex per-host agar kartu kontroler OLT tidak
// kewalahan menerima sesi CLI paralel.
func HostMutex(host string) *sync.Mutex {
	val, _ := hostMutexMap.LoadOrStore(host, &sync.Mutex{})
	return val.(*sync.Mutex)
}

// Options mengonfigurasi handshake dan framing sesi CLI.
type Options struct {
	Host     string
	Port     int
	Username string
	Password string

	// TimeoutSeconds berlaku untuk handshake maupun tiap perintah.
	TimeoutSeconds int

	// UsernamePrompts adalah penanda yang menandai OLT meminta username.
	UsernamePrompts []string
	// PasswordPrompts adalah penanda yang menandai OLT meminta password.
	PasswordPrompts []string

	// EnableCommand dikirim bila prompt awal bukan privileged. Kosongkan bila
	// perangkat tidak memakai konsep "enable".
	EnableCommand string
	// EnablePasswordPrompt adalah penanda yang menandai OLT meminta password
	// untuk perintah enable (bila ada).
	EnablePasswordPrompt string

	// PromptRe mengenali baris prompt CLI.
	PromptRe *regexp.Regexp
	// DisablePagingCommand dikirim setelah login untuk mematikan paging.
	DisablePagingCommand string
}

// Client adalah klien CLI Telnet.
type Client struct {
	opts Options
}

// New membuat klien CLI dengan opsi default yang overridden oleh opts.
func New(opts Options) *Client {
	if opts.Port <= 0 {
		opts.Port = 23
	}
	if opts.TimeoutSeconds <= 0 {
		opts.TimeoutSeconds = 15
	}
	if len(opts.UsernamePrompts) == 0 {
		opts.UsernamePrompts = []string{"username:", "user name:", "login:"}
	}
	if len(opts.PasswordPrompts) == 0 {
		opts.PasswordPrompts = []string{"password:"}
	}
	if opts.PromptRe == nil {
		opts.PromptRe = regexp.MustCompile(`(?m)^[A-Za-z][A-Za-z0-9_.\-]*[#>\]]\s*$`)
	}
	return &Client{opts: opts}
}

// Session adalah sesi CLI aktif.
type Session struct {
	conn    net.Conn
	reader  *bufio.Reader
	prompt  string
	timeout time.Duration
	opts    Options
}

func (s *Session) Close() {
	if s.conn != nil {
		_ = s.conn.Close()
	}
}

// Execute menjalankan satu perintah pada sesi BARU (login → perintah → logout).
func (c *Client) Execute(ctx context.Context, command string) (string, error) {
	mu := HostMutex(c.opts.Host)
	mu.Lock()
	defer mu.Unlock()

	sess, err := c.Dial(ctx)
	if err != nil {
		return "", err
	}
	defer sess.Close()

	return sess.Execute(ctx, command)
}

// ExecuteScript menjalankan rangkaian perintah pada SATU sesi.
//
// Perintah dikirim satu per satu (bukan dilumpuhkan) agar output tiap langkah
// dapat diperiksa dan agar kartu kontroler tidak kewalahan menerima burst
// input. Ini penting untuk provisioning perangkat.
func (c *Client) ExecuteScript(ctx context.Context, commands []string) (string, error) {
	mu := HostMutex(c.opts.Host)
	mu.Lock()
	defer mu.Unlock()

	sess, err := c.Dial(ctx)
	if err != nil {
		return "", err
	}
	defer sess.Close()

	var sb strings.Builder
	for _, cmd := range commands {
		out, err := sess.Execute(ctx, cmd)
		sb.WriteString(out)
		sb.WriteString("\n")
		if err != nil {
			return sb.String(), fmt.Errorf("perintah '%s' gagal: %w", cmd, err)
		}
	}
	return sb.String(), nil
}

// Dial membuka koneksi & melakukan handshake.
func (c *Client) Dial(ctx context.Context) (*Session, error) {
	timeout := time.Duration(c.opts.TimeoutSeconds) * time.Second
	addr := net.JoinHostPort(c.opts.Host, strconv.Itoa(c.opts.Port))

	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, fmt.Errorf("gagal terhubung ke %s: %w", addr, err)
	}

	sess := &Session{
		conn:    conn,
		reader:  bufio.NewReader(conn),
		timeout: timeout,
		opts:    c.opts,
	}

	fail := func(stage string, err error) (*Session, error) {
		_ = conn.Close()
		return nil, fmt.Errorf("gagal saat handshake %s: %w", stage, err)
	}

	// 1. Username
	if _, err := sess.readUntil(ctx, c.opts.UsernamePrompts...); err != nil {
		return fail("username", err)
	}
	_, _ = conn.Write([]byte(c.opts.Username + "\r\n"))

	// 2. Password
	if _, err := sess.readUntil(ctx, c.opts.PasswordPrompts...); err != nil {
		return fail("password", err)
	}
	_, _ = conn.Write([]byte(c.opts.Password + "\r\n"))

	// 3. Prompt awal
	out, err := sess.readUntilMatch(ctx, c.opts.PromptRe)
	if err != nil {
		return fail("login", err)
	}
	sess.prompt = extractPrompt(out, c.opts.PromptRe)
	if sess.prompt == "" {
		sess.prompt = "#"
	}

	// 4. Escalate privilege bila perlu
	if c.opts.EnableCommand != "" && strings.HasSuffix(sess.prompt, ">") {
		_, _ = conn.Write([]byte(c.opts.EnableCommand + "\r\n"))
		out, err := sess.readUntilMatch(ctx, c.opts.PromptRe)
		if err != nil {
			return fail(c.opts.EnableCommand, err)
		}
		if c.opts.EnablePasswordPrompt != "" && strings.Contains(strings.ToLower(out), strings.ToLower(c.opts.EnablePasswordPrompt)) {
			_, _ = conn.Write([]byte(c.opts.Password + "\r\n"))
			out, err = sess.readUntilMatch(ctx, c.opts.PromptRe)
			if err != nil {
				return fail(c.opts.EnableCommand, err)
			}
		}
		if p := extractPrompt(out, c.opts.PromptRe); p != "" {
			sess.prompt = p
		}
	}

	// 5. Disable paging
	if c.opts.DisablePagingCommand != "" {
		_, _ = conn.Write([]byte(c.opts.DisablePagingCommand + "\r\n"))
		if _, err := sess.readUntilMatch(ctx, c.opts.PromptRe); err != nil {
			return fail(c.opts.DisablePagingCommand, err)
		}
	}

	return sess, nil
}

// Execute mengirim satu perintah dan membaca output sampai prompt kembali.
func (s *Session) Execute(ctx context.Context, command string) (string, error) {
	_, _ = s.conn.Write([]byte(command + "\r\n"))

	raw, err := s.readUntil(ctx, s.prompt)
	if err != nil {
		return raw, fmt.Errorf("gagal membaca output perintah '%s': %w", command, err)
	}

	return CleanOutput(raw, command, s.prompt), nil
}

// readUntil membaca byte demi byte sampai salah satu delimiter ditemukan.
//
// Implementasi memakai deadline socket + matcher inkremental pada buffer
// "tail": linear, race-free, dan tidak bocorkan goroutine.
func (s *Session) readUntil(ctx context.Context, delims ...string) (string, error) {
	var buf strings.Builder

	maxDelim := 0
	for _, d := range delims {
		if len(d) > maxDelim {
			maxDelim = len(d)
		}
	}
	tailSize := maxDelim + 4
	tail := make([]byte, 0, tailSize)
	lowerDelims := make([]string, len(delims))
	for i, d := range delims {
		lowerDelims[i] = strings.ToLower(d)
	}

	for {
		select {
		case <-ctx.Done():
			return buf.String(), ctx.Err()
		default:
		}

		if err := s.conn.SetReadDeadline(time.Now().Add(s.timeout)); err != nil {
			return buf.String(), fmt.Errorf("gagal set read deadline: %w", err)
		}

		b, err := s.reader.ReadByte()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				return buf.String(), fmt.Errorf("timeout menunggu prompt %v", delims)
			}
			return buf.String(), err
		}

		buf.WriteByte(b)
		tail = append(tail, b)
		if len(tail) > tailSize {
			tail = tail[len(tail)-tailSize:]
		}

		lower := strings.ToLower(string(tail))
		for _, d := range lowerDelims {
			if strings.Contains(lower, d) {
				return buf.String(), nil
			}
		}
	}
}

// readUntilMatch menghentikan pembacaan saat tail buffer cocok dengan regex.
func (s *Session) readUntilMatch(ctx context.Context, re *regexp.Regexp) (string, error) {
	var buf strings.Builder
	const tailSize = 256
	tail := make([]byte, 0, tailSize)

	for {
		select {
		case <-ctx.Done():
			return buf.String(), ctx.Err()
		default:
		}

		if err := s.conn.SetReadDeadline(time.Now().Add(s.timeout)); err != nil {
			return buf.String(), fmt.Errorf("gagal set read deadline: %w", err)
		}

		b, err := s.reader.ReadByte()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				return buf.String(), fmt.Errorf("timeout menunggu prompt")
			}
			return buf.String(), err
		}

		buf.WriteByte(b)
		tail = append(tail, b)
		if len(tail) > tailSize {
			tail = tail[len(tail)-tailSize:]
		}

		if re.Match(tail) {
			return buf.String(), nil
		}
	}
}

// extractPrompt mengambil baris prompt terakhir dari output handshake.
func extractPrompt(out string, re *regexp.Regexp) string {
	lines := strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		t := strings.TrimSpace(lines[i])
		if t == "" {
			continue
		}
		if re.MatchString(t) {
			return t
		}
	}
	return ""
}

// CleanOutput membuang echo perintah, byte NUL, baris kosong di tepi, dan
// prompt di ekor output.
func CleanOutput(raw, command, prompt string) string {
	raw = strings.ReplaceAll(raw, "\x00", "")
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(raw, "\n")

	out := make([]string, 0, len(lines))
	echoSkipped := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !echoSkipped && trimmed != "" {
			if command == "" || strings.Contains(trimmed, command) {
				echoSkipped = true
				continue
			}
		}
		if prompt != "" && trimmed == prompt {
			continue
		}
		out = append(out, strings.TrimRight(line, "\r"))
	}

	for len(out) > 0 && strings.TrimSpace(out[0]) == "" {
		out = out[1:]
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}

// LooksLikeCommandError mendeteksi tanda perangkat menolak sebuah perintah.
//
// Pola yang teramati pada OLT ZTE & C-Data:
//   - "%Error 20204: Ambiguous command: ..."
//   - "%Error 20203: Incomplete command."
//   - "%Error 20200: Invalid input detected at '^' marker.Invalid command"
//   - "Invalid parameter"
func LooksLikeCommandError(output string) bool {
	lower := strings.ToLower(output)
	for _, marker := range []string{
		"%error",
		"invalid command",
		"invalid parameter",
		"incomplete command",
		"ambiguous command",
		"unknown command",
		"not supported",
		"no such command",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// FirstSupported menjalankan perintah dari sebuah rantai fallback dan
// mengembalikan output pertama yang TIDAK ditolak perangkat.
//
// Rantai fallback penting karena varian OLT C-Data (FD11/FD12 vs FD16xx)
// maupun ZTE (C300 vs C320) berbeda dukungan perintah.
func FirstSupported(ctx context.Context, c *Client, commands []string) (string, string, error) {
	var lastErr error
	for _, cmd := range commands {
		out, err := c.Execute(ctx, cmd)
		if err != nil {
			lastErr = err
			continue
		}
		if LooksLikeCommandError(out) {
			lastErr = fmt.Errorf("perintah '%s' ditolak perangkat", cmd)
			continue
		}
		return out, cmd, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("tidak ada perintah yang berhasil dieksekusi")
	}
	return "", "", lastErr
}
