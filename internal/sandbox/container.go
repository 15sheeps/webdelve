package sandbox

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"github.com/moby/moby/client"
	"io"
)

// Container represents running container
type Container struct {
	id       string
	hostPort string // dynamically assigned port for delve
	cli      *client.Client
}

// HostPort returns delve's listening port mapped to host
func (c *Container) HostPort() string {
	return c.hostPort
}

// Returns the ID of the container
func (c *Container) ID() string {
	return c.id
}

// remove forcefully removes the container.
func (c *Container) remove(ctx context.Context) (err error) {
	_, err = c.cli.ContainerRemove(ctx, c.id, client.ContainerRemoveOptions{Force: true})
	return
}

// HealthCheck verifies that the container is still in a running state.
func (c *Container) HealthCheck(ctx context.Context) error {
	info, err := c.cli.ContainerInspect(ctx, c.id, client.ContainerInspectOptions{})
	if err != nil {
		return fmt.Errorf("container inspect failed: %w", err)
	}
	if !info.Container.State.Running {
		return fmt.Errorf("container %s is not running (status: %s)", c.id, info.Container.State.Status)
	}

	return nil
}

// CopyFileTo copies file to the container at specified path
func (c *Container) CopyFileTo(ctx context.Context, path string, src []byte) error {
	buf := new(bytes.Buffer)
	tw := tar.NewWriter(buf)

	hdr := &tar.Header{
		Name: path,
		Mode: 0644,
		Size: int64(len(src)),
	}

	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}

	if _, err := tw.Write(src); err != nil {
		return err
	}

	if err := tw.Close(); err != nil {
		return err
	}

	_, err := c.cli.CopyToContainer(ctx, c.id, client.CopyToContainerOptions{
		DestinationPath:           "/",
		Content:                   buf,
		AllowOverwriteDirWithFile: true,
	})

	return err
}

func (c *Container) Exec(ctx context.Context, cmd []string) (io.Reader, error) {
	execCfg := client.ExecCreateOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	}

	execResp, err := c.cli.ExecCreate(ctx, c.id, execCfg)
	if err != nil {
		return nil, fmt.Errorf("exec create failed: %w", err)
	}

	attachResp, err := c.cli.ExecAttach(ctx, execResp.ID, client.ExecAttachOptions{})
	if err != nil {
		return nil, fmt.Errorf("exec attach failed: %w", err)
	}

	return attachResp.Reader, nil
}

func (c *Container) StreamLogs(ctx context.Context) (io.ReadCloser, error) {
	return c.cli.ContainerLogs(ctx, c.id, client.ContainerLogsOptions{
		ShowStdout: true,
		Follow:     true,
	})
}
