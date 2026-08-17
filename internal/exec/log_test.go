package exec

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLogWriterRoundTripsRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exec.log")
	w, err := NewLogWriter(path, 1024)
	if err != nil {
		t.Fatalf("NewLogWriter() error = %v", err)
	}
	if _, err := w.Append(LogStdout, []byte("hello")); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if _, err := w.Append(LogStderr, []byte("boom")); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	records, err := ReadRecords(path, 0)
	if err != nil {
		t.Fatalf("ReadRecords() error = %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(records))
	}
	if records[0].Stream != LogStdout || !bytes.Equal(records[0].Data, []byte("hello")) {
		t.Errorf("records[0] = %+v, want stdout hello", records[0])
	}
	if records[1].Stream != LogStderr || !bytes.Equal(records[1].Data, []byte("boom")) {
		t.Errorf("records[1] = %+v, want stderr boom", records[1])
	}
}

func TestReadRecordsResumesFromOffset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exec.log")
	w, _ := NewLogWriter(path, 1024)
	first, _ := w.Append(LogStdout, []byte("aaa"))
	w.Append(LogStdout, []byte("bbb"))
	w.Close()

	records, err := ReadRecords(path, first)
	if err != nil {
		t.Fatalf("ReadRecords() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	if !bytes.Equal(records[0].Data, []byte("bbb")) {
		t.Errorf("records[0].Data = %q, want bbb", records[0].Data)
	}
}

func TestAppendReturnsOffsetAfterRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exec.log")
	w, _ := NewLogWriter(path, 1024)
	defer w.Close()

	off, _ := w.Append(LogStdout, []byte("12345"))
	if off != 10 {
		t.Errorf("offset = %d, want 10 (5 header + 5 payload)", off)
	}
	if w.Offset() != off {
		t.Errorf("Offset() = %d, want %d", w.Offset(), off)
	}
}

func TestAppendStopsAtMaxBytesAndMarksTruncated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exec.log")
	w, _ := NewLogWriter(path, 12)
	defer w.Close()

	w.Append(LogStdout, []byte("12345"))
	if w.Truncated() {
		t.Fatal("Truncated() = true after first append, want false")
	}
	w.Append(LogStdout, []byte("67890"))
	if !w.Truncated() {
		t.Fatal("Truncated() = false after exceeding cap, want true")
	}

	records, _ := ReadRecords(path, 0)
	if len(records) != 1 {
		t.Errorf("len(records) = %d, want 1 (second append dropped)", len(records))
	}
}

func TestAppendRejectsOversizedPayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exec.log")
	w, _ := NewLogWriter(path, 1<<20)
	defer w.Close()

	if _, err := w.Append(LogStdout, make([]byte, MaxFramePayload+1)); err == nil {
		t.Fatal("Append() error = nil, want error for oversized payload")
	}
}

func TestGapRecordRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exec.log")
	w, _ := NewLogWriter(path, 1024)
	w.Append(LogGap, nil)
	w.Close()

	records, _ := ReadRecords(path, 0)
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	if records[0].Stream != LogGap {
		t.Errorf("Stream = %d, want LogGap", records[0].Stream)
	}
	if len(records[0].Data) != 0 {
		t.Errorf("Data = %q, want empty", records[0].Data)
	}
}

func TestTornTailIsRepairedOnReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exec.log")
	w, _ := NewLogWriter(path, 1024)
	w.Append(LogStdout, []byte("first"))
	w.Append(LogStdout, []byte("second"))
	w.Close()

	f, _ := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	buf := make([]byte, 5)
	buf[0] = byte(LogStdout)
	binary.BigEndian.PutUint32(buf[1:], 100)
	f.Write(buf)
	f.Close()

	w2, _ := NewLogWriter(path, 1024)
	w2.Append(LogStdout, []byte("third"))
	w2.Close()

	records, _ := ReadRecords(path, 0)
	if len(records) != 3 {
		t.Fatalf("len(records) = %d, want 3", len(records))
	}
	if !bytes.Equal(records[0].Data, []byte("first")) || !bytes.Equal(records[1].Data, []byte("second")) || !bytes.Equal(records[2].Data, []byte("third")) {
		t.Errorf("records = %+v, want [first second third]", records)
	}
}

func TestAppendAtExactBoundaryIsWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exec.log")
	w, _ := NewLogWriter(path, 21)
	defer w.Close()

	w.Append(LogStdout, []byte("12345"))
	if w.Truncated() {
		t.Fatal("Truncated() = true after first append (10 bytes), want false")
	}

	off, err := w.Append(LogStdout, []byte("123456"))
	if err != nil {
		t.Fatalf("Append() error = %v, want nil (offset+size == maxBytes)", err)
	}
	if w.Truncated() {
		t.Fatal("Truncated() = true at exact boundary (10+11=21==maxBytes), want false")
	}
	if off != 21 {
		t.Errorf("offset = %d, want 21", off)
	}

	records, _ := ReadRecords(path, 0)
	if len(records) != 2 {
		t.Errorf("len(records) = %d, want 2 (both records written)", len(records))
	}
}

func TestAppendOverBoundaryByOneByteIsTruncated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exec.log")
	w, _ := NewLogWriter(path, 20)
	defer w.Close()

	w.Append(LogStdout, []byte("12345"))
	if w.Truncated() {
		t.Fatal("Truncated() = true after first append (10 bytes), want false")
	}

	w.Append(LogStdout, []byte("123456"))
	if !w.Truncated() {
		t.Fatal("Truncated() = false at maxBytes+1 (10+11=21>20), want true")
	}

	records, _ := ReadRecords(path, 0)
	if len(records) != 1 {
		t.Errorf("len(records) = %d, want 1 (second append refused)", len(records))
	}
}

func TestFailedWriterRefusesFurtherAppends(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exec.log")
	w, _ := NewLogWriter(path, 1024)
	w.Append(LogStdout, []byte("ok"))

	w.Close()
	os.Remove(path)
	os.Mkdir(path, 0o755)

	_, err := w.Append(LogStdout, []byte("test"))
	if err == nil {
		t.Fatal("Append() error = nil after path becomes dir, want error")
	}
	if !errors.Is(err, ErrWriterFailed) {
		t.Errorf("error = %v, want ErrWriterFailed", err)
	}

	_, err2 := w.Append(LogStdout, []byte("test2"))
	if !errors.Is(err2, ErrWriterFailed) {
		t.Errorf("second Append() error = %v, want ErrWriterFailed", err2)
	}
}

func TestFailedAppendErrorWrapsUnderlyingCause(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exec.log")
	w, _ := NewLogWriter(path, 1024)
	w.Append(LogStdout, []byte("ok"))
	w.Close()

	_, err := w.Append(LogStdout, []byte("test"))
	if err == nil {
		t.Fatal("Append() error = nil after Close(), want error")
	}
	if !errors.Is(err, ErrWriterFailed) {
		t.Errorf("error = %v, want errors.Is ErrWriterFailed", err)
	}
	if !errors.Is(err, os.ErrClosed) {
		t.Errorf("error = %v, want errors.Is os.ErrClosed (underlying cause must survive)", err)
	}
}

func TestRepairTornTailRejectsOversizedLength(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exec.log")
	w, _ := NewLogWriter(path, 1<<20)
	w.Append(LogStdout, []byte("first"))
	w.Close()

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	header := make([]byte, headerSize)
	header[0] = byte(LogStdout)
	binary.BigEndian.PutUint32(header[1:], MaxFramePayload+1)
	f.Write(header)
	f.Write(bytes.Repeat([]byte{'x'}, MaxFramePayload+1))
	f.Close()

	w2, err := NewLogWriter(path, 1<<20)
	if err != nil {
		t.Fatalf("NewLogWriter() error = %v", err)
	}
	w2.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Size() != 10 {
		t.Fatalf("file size after repair = %d, want 10 (oversized record truncated away)", info.Size())
	}

	records, err := ReadRecords(path, 0)
	if err != nil {
		t.Fatalf("ReadRecords() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	if !bytes.Equal(records[0].Data, []byte("first")) {
		t.Errorf("records[0].Data = %q, want first", records[0].Data)
	}
}

func TestReadRecordsStopsAtOversizedLength(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exec.log")
	w, _ := NewLogWriter(path, 1<<20)
	w.Append(LogStdout, []byte("first"))
	w.Close()

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	header := make([]byte, headerSize)
	header[0] = byte(LogStdout)
	binary.BigEndian.PutUint32(header[1:], MaxFramePayload+1)
	f.Write(header)
	f.Write(bytes.Repeat([]byte{'x'}, MaxFramePayload+1))
	f.Close()

	records, err := ReadRecords(path, 0)
	if err != nil {
		t.Fatalf("ReadRecords() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1 (oversized record rejected)", len(records))
	}
	if !bytes.Equal(records[0].Data, []byte("first")) {
		t.Errorf("records[0].Data = %q, want first", records[0].Data)
	}
}

func TestRepairErrorAbortsWithoutTruncating(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission checks, cannot force a read-open error this way")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "exec.log")
	w, _ := NewLogWriter(path, 1024)
	w.Append(LogStdout, []byte("hello"))
	w.Close()

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if err := os.Chmod(path, 0o200); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	defer os.Chmod(path, 0o600)

	_, err = NewLogWriter(path, 1024)
	if err == nil {
		t.Fatal("NewLogWriter() error = nil, want error from failed repair scan")
	}

	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("Chmod() restore error = %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("file contents changed after failed repair: before=%d bytes after=%d bytes", len(before), len(after))
	}
}
