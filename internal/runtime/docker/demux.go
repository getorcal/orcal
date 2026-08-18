package docker

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/getorcal/orcal/internal/runtime"
)

const dockerHeaderSize = 8

func demux(r io.Reader) func() (runtime.Frame, error) {
	header := make([]byte, dockerHeaderSize)
	return func() (runtime.Frame, error) {
		if _, err := io.ReadFull(r, header); err != nil {
			if err == io.ErrUnexpectedEOF {
				return runtime.Frame{}, fmt.Errorf("docker: truncated stream header: %w", err)
			}
			return runtime.Frame{}, err
		}
		size := binary.BigEndian.Uint32(header[4:])
		data := make([]byte, size)
		if _, err := io.ReadFull(r, data); err != nil {
			return runtime.Frame{}, fmt.Errorf("docker: truncated stream payload: %w", err)
		}
		stream := runtime.StreamStdout
		if header[0] == 2 {
			stream = runtime.StreamStderr
		}
		return runtime.Frame{Stream: stream, Data: data}, nil
	}
}
