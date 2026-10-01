package billing

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// walRecord is one settlement made while Redis was unreachable. It carries
// the exact KEYS and ARGV of the finalize script, so replay is the same
// atomic step the gateway would have run, and the script's done marker makes
// replaying a record twice harmless.
type walRecord struct {
	Keys []string `json:"k"`
	Args []string `json:"a"`
}

type walReq struct {
	line []byte
	done chan error
}

// wal is an append-only log of segments wal-<seq>.log. Appends are group
// committed: one fsync covers every record queued meanwhile, and an append
// returns only once its record is durable.
type wal struct {
	dir     string
	reqs    chan walReq
	stop    chan struct{}
	stopped chan struct{}
	pending atomic.Int64 // records not yet replayed, including older segments

	mu  sync.Mutex // guards f and seq
	f   *os.File
	seq int64
}

const walBatch = 256

func openWAL(dir string) (*wal, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("billing: wal dir: %w", err)
	}
	w := &wal{dir: dir, reqs: make(chan walReq, walBatch), stop: make(chan struct{}), stopped: make(chan struct{})}
	segs, err := w.segments()
	if err != nil {
		return nil, err
	}
	for _, s := range segs {
		w.seq = max(w.seq, s.seq)
		n, err := countLines(s.path)
		if err != nil {
			return nil, err
		}
		w.pending.Add(n)
	}
	if err := w.openNext(); err != nil {
		return nil, err
	}
	go w.writer()
	return w, nil
}

// openNext starts a new segment; existing segments are only ever replayed.
func (w *wal) openNext() error {
	w.seq++
	f, err := os.OpenFile(w.path(w.seq), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("billing: open wal segment: %w", err)
	}
	w.f = f
	return nil
}

func (w *wal) path(seq int64) string { return filepath.Join(w.dir, fmt.Sprintf("wal-%012d.log", seq)) }

var errWALClosed = errors.New("billing: wal closed")

// append makes rec durable before returning.
func (w *wal) append(rec walRecord) error {
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	req := walReq{line: append(line, '\n'), done: make(chan error, 1)}
	select {
	case w.reqs <- req:
	case <-w.stop:
		return errWALClosed
	}
	select {
	case err := <-req.done:
		return err
	case <-time.After(10 * time.Second):
		return errors.New("billing: wal append timed out")
	}
}

func (w *wal) writer() {
	defer close(w.stopped)
	for {
		var first walReq
		select {
		case first = <-w.reqs:
		case <-w.stop:
			return
		}
		batch := []walReq{first}
	drain:
		for len(batch) < walBatch {
			select {
			case r := <-w.reqs:
				batch = append(batch, r)
			default:
				break drain
			}
		}
		err := w.writeBatch(batch)
		for _, r := range batch {
			r.done <- err
		}
	}
}

func (w *wal) writeBatch(batch []walReq) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, r := range batch {
		if _, err := w.f.Write(r.line); err != nil {
			return fmt.Errorf("billing: wal write: %w", err)
		}
	}
	if err := w.f.Sync(); err != nil {
		return fmt.Errorf("billing: wal fsync: %w", err)
	}
	w.pending.Add(int64(len(batch)))
	return nil
}

type segment struct {
	seq  int64
	path string
}

func (w *wal) segments() ([]segment, error) {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return nil, fmt.Errorf("billing: list wal: %w", err)
	}
	var out []segment
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "wal-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		seq, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(name, "wal-"), ".log"), 10, 64)
		if err == nil {
			out = append(out, segment{seq: seq, path: filepath.Join(w.dir, name)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].seq < out[j].seq })
	return out, nil
}

// sealed closes the active segment if it has records and returns every
// segment that is no longer written to, oldest first.
func (w *wal) sealed() ([]segment, error) {
	w.mu.Lock()
	if info, err := w.f.Stat(); err == nil && info.Size() > 0 {
		_ = w.f.Close()
		if err := w.openNext(); err != nil {
			w.mu.Unlock()
			return nil, err
		}
	}
	active := w.seq
	w.mu.Unlock()
	segs, err := w.segments()
	if err != nil {
		return nil, err
	}
	out := segs[:0]
	for _, s := range segs {
		if s.seq != active {
			out = append(out, s)
		}
	}
	return out, nil
}

// readSegment parses a segment. A torn last line (crash mid-write, never
// acknowledged) is skipped; any other bad line is an error.
func readSegment(path string) ([]walRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	r := bufio.NewReaderSize(f, 1<<20)
	var out []walRecord
	for {
		line, err := r.ReadBytes('\n')
		if errors.Is(err, io.EOF) {
			return out, nil // len(line) > 0 here means a torn, unacknowledged tail
		}
		if err != nil {
			return nil, err
		}
		var rec walRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, fmt.Errorf("billing: corrupt wal record in %s: %w", path, err)
		}
		out = append(out, rec)
	}
}

func countLines(path string) (int64, error) {
	recs, err := readSegment(path)
	return int64(len(recs)), err
}

func (w *wal) close() {
	close(w.stop)
	<-w.stopped
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.f.Close()
}
