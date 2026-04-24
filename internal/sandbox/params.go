package sandbox

import (
	"github.com/15sheeps/webdelve/internal/config"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"net/netip"
)

var (
	delvePort = network.MustParsePort("2345/tcp")
	localhost = netip.MustParseAddr("127.0.0.1")
)

func CreateOptions(cfg config.SandboxConfig) client.ContainerCreateOptions {
	return client.ContainerCreateOptions{
		Config: &container.Config{
			Image: cfg.Image,
			ExposedPorts: network.PortSet{
				delvePort: struct{}{},
			},
			AttachStdin:  false,
			AttachStdout: true,
			AttachStderr: true,
			// this will allow to attach standard streams to a tty
			// and copy data directly using ContainerLogs()
			Tty: true,
		},
		HostConfig: &container.HostConfig{
			PortBindings: network.PortMap{
				delvePort: []network.PortBinding{
					{
						HostIP:   localhost,
						HostPort: "",
					},
				},
			},
			Tmpfs: map[string]string{
				"/tmpfs": "exec,rw,nosuid,size=100M",
			},
			Runtime:     "runsc",
			NetworkMode: network.NetworkDefault,
			Resources: container.Resources{
				Memory: cfg.MemoryLimit,
			},
			AutoRemove: true,
		},
	}
}
