package exec

import (
	"bytes"
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
