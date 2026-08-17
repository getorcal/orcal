package exec

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

type LogStream byte

const (
	LogStdout LogStream = 0x01
	LogStderr LogStream = 0x02
	LogGap    LogStream = 0x03
)

const (
	MaxFramePayload = 64 * 1024
	headerSize      = 5
)

type Record struct {
	Stream LogStream
	Data   []byte
	Offset int64
}

type LogWriter struct {
	mu        sync.Mutex
	f         *os.File
	offset    int64
	maxBytes  int64
	truncated bool
}

func NewLogWriter(path string, maxBytes int64) (*LogWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("exec: open log: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("exec: stat log: %w", err)
	}
	return &LogWriter{f: f, offset: info.Size(), maxBytes: maxBytes}, nil
}

func (w *LogWriter) Append(stream LogStream, data []byte) (int64, error) {
	if len(data) > MaxFramePayload {
		return 0, fmt.Errorf("exec: payload %d exceeds max frame %d", len(data), MaxFramePayload)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.truncated {
		return w.offset, nil
	}
	size := int64(headerSize + len(data))
	if w.offset+size > w.maxBytes {
		w.truncated = true
		return w.offset, nil
	}

	buf := make([]byte, headerSize+len(data))
	buf[0] = byte(stream)
	binary.BigEndian.PutUint32(buf[1:headerSize], uint32(len(data)))
	copy(buf[headerSize:], data)

	if _, err := w.f.Write(buf); err != nil {
		return w.offset, fmt.Errorf("exec: write log: %w", err)
	}
	w.offset += size
	return w.offset, nil
}

func (w *LogWriter) Offset() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.offset
}

func (w *LogWriter) Truncated() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.truncated
}

func (w *LogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.f.Close()
}

func ReadRecords(path string, from int64) ([]Record, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("exec: open log: %w", err)
	}
	defer f.Close()

	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return nil, fmt.Errorf("exec: seek log: %w", err)
	}

	var (
		records []Record
		offset  = from
		header  = make([]byte, headerSize)
	)
	for {
		if _, err := io.ReadFull(f, header); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return records, nil
			}
			return nil, fmt.Errorf("exec: read log header: %w", err)
		}
		n := binary.BigEndian.Uint32(header[1:])
		data := make([]byte, n)
		if _, err := io.ReadFull(f, data); err != nil {
			return records, nil
		}
		offset += int64(headerSize) + int64(n)
		records = append(records, Record{Stream: LogStream(header[0]), Data: data, Offset: offset})
	}
}
