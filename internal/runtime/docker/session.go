package docker

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"

	"github.com/getorcal/orcal/internal/runtime"
)

const execPollInterval = 100 * time.Millisecond

type session struct {
	cli      *client.Client
	execID   string
	attached types.HijackedResponse
	next     func() (runtime.Frame, error)
}

func newSession(cli *client.Client, execID string, attached types.HijackedResponse) *session {
	return &session{
		cli:      cli,
		execID:   execID,
		attached: attached,
		next:     demux(attached.Reader),
	}
}

func (s *session) ID() string { return s.execID }

func (s *session) Recv() (runtime.Frame, error) { return s.next() }

func (s *session) Wait(ctx context.Context) (int, error) {
	ticker := time.NewTicker(execPollInterval)
	defer ticker.Stop()
	for {
		info, err := s.cli.ContainerExecInspect(ctx, s.execID)
		if err != nil {
			return 0, translate(err)
		}
		if !info.Running {
			return info.ExitCode, nil
		}
		select {
		case <-ctx.Done():
			return 0, fmt.Errorf("docker: waiting for exec %s: %w", s.execID, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (s *session) Close() error {
	s.attached.Close()
	return nil
}
