package kiosk

import (
	"fmt"
	"io"
	"os"
	"sync"
)

// DefaultLogSize is how much of the kiosk journal is kept, per generation.
//
// One line is about a hundred bytes, so a quarter of a megabyte holds a couple of
// thousand of them — years of ordinary starts, or two days of a browser that dies in a
// loop. Two generations bound the whole thing at half a megabyte, which is what makes the
// file safe to leave on a station nobody visits (§10.4).
const DefaultLogSize = 256 * 1024

// journal is the kiosk log file, bounded in size.
//
// It rotates rather than truncates: the interesting lines of a station that failed at
// boot are the FIRST ones, and a file that dropped its head to keep its tail would throw
// away the sentence that names the cause. One previous generation is kept, under the same
// name with « .1 » appended, which is the shape update.ps1 already uses for the binary.
type journal struct {
	// mu guards everything below. The supervisor logs from its supervision loop AND from
	// the goroutine that renews the sleep inhibition: two writers is the ordinary shape.
	mu       sync.Mutex
	file     *os.File
	path     string
	maxBytes int64
	size     int64
}

// OpenLog opens the kiosk journal, creating it if needed and appending to it.
//
// It returns an error rather than a silent no-op when the file cannot be opened: the
// caller decides what to do about it, and on a station the answer is « say it on the
// standard output and show the client screen anyway » — a journal is a diagnostic aid,
// never a reason to leave a customer in front of a black screen.
func OpenLog(path string, maxBytes int64) (io.WriteCloser, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("le journal du kiosque %s n'a pas pu être ouvert : %w", path, err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("la taille du journal du kiosque %s est illisible : %w", path, err)
	}
	return &journal{file: file, path: path, maxBytes: maxBytes, size: info.Size()}, nil
}

// Write appends one write to the journal, rotating first when it would not fit.
//
// The whole write goes to one generation or the other, never split across the rotation:
// a line cut in half is a line nobody can read, and the point of this file is to be read
// six months later by somebody who was not there.
func (j *journal) Write(p []byte) (int, error) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.size > 0 && j.size+int64(len(p)) > j.maxBytes {
		if err := j.rotate(); err != nil {
			return 0, err
		}
	}
	written, err := j.file.Write(p)
	j.size += int64(written)
	return written, err
}

// Close closes the journal.
func (j *journal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.file.Close()
}

// rotate moves the current generation aside and starts an empty one.
func (j *journal) rotate() error {
	if err := j.file.Close(); err != nil {
		return err
	}
	// Rename over any previous generation: two generations, always, and no third file
	// appearing on a station where nobody prunes anything.
	if err := os.Rename(j.path, j.path+".1"); err != nil {
		return err
	}
	file, err := os.OpenFile(j.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	j.file, j.size = file, 0
	return nil
}
