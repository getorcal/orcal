package docker

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/getorcal/orcal/internal/runtime"
)

func dockerFrame(stream byte, payload string) []byte {
	frame := make([]byte, 8+len(payload))
	frame[0] = stream
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(payload)))
	copy(frame[8:], payload)
	return frame
}

func TestDemuxSplitsStdoutAndStderr(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(dockerFrame(1, "out"))
	buf.Write(dockerFrame(2, "err"))
	next := demux(&buf)

	first, err := next()
	if err != nil {
		t.Fatalf("first frame error = %v", err)
	}
	if first.Stream != runtime.StreamStdout || string(first.Data) != "out" {
		t.Errorf("first = %+v, want stdout out", first)
	}

	second, err := next()
	if err != nil {
		t.Fatalf("second frame error = %v", err)
	}
	if second.Stream != runtime.StreamStderr || string(second.Data) != "err" {
		t.Errorf("second = %+v, want stderr err", second)
	}

	if _, err := next(); !errors.Is(err, io.EOF) {
		t.Errorf("third frame error = %v, want io.EOF", err)
	}
}

func TestDemuxHandlesEmptyPayload(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(dockerFrame(1, ""))
	next := demux(&buf)

	frame, err := next()
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(frame.Data) != 0 {
		t.Errorf("Data = %q, want empty", frame.Data)
	}
}

func TestDemuxReturnsEOFOnTruncatedHeader(t *testing.T) {
	next := demux(bytes.NewReader([]byte{1, 0, 0}))

	if _, err := next(); err == nil {
		t.Fatal("error = nil, want a read error on a truncated header")
	}
}

func TestDemuxRejectsOversizedFrame(t *testing.T) {
	header := make([]byte, 8)
	header[0] = 1
	binary.BigEndian.PutUint32(header[4:], 1<<30)
	next := demux(bytes.NewReader(header))

	if _, err := next(); err == nil {
		t.Fatal("error = nil, want rejection of an oversized frame")
	}
}

func TestDemuxRejectsOversizedFrameWithoutReadingPayload(t *testing.T) {
	pr, pw := io.Pipe()
	header := make([]byte, 8)
	header[0] = 1
	binary.BigEndian.PutUint32(header[4:], 1<<30)
	go func() {
		pw.Write(header)
	}()
	next := demux(pr)

	done := make(chan error, 1)
	go func() {
		_, err := next()
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("error = nil, want rejection of an oversized frame")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("demux blocked reading an oversized payload instead of rejecting the size header")
	}
}
