package sandbox

import (
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"net/netip"
)

var (
	memoryLimit   = int64(100 << 20) // 100 MB
	maxBinarySize = int64(50 << 20)  // 50 MB
	maxSourceSize = int64(1 << 20)   // 1 MB
	delvePort     = network.MustParsePort("2345/tcp")
)

func GetSandboxConfigs() client.ContainerCreateOptions {
	cfg := &container.Config{
		Image: "runtime-image:latest",
		Cmd:   []string{"sleep", "infinity"},
		ExposedPorts: network.PortSet{
			delvePort: struct{}{},
		},
		AttachStdin:  false,
		AttachStdout: true,
		AttachStderr: true,
	}

	hostCfg := &container.HostConfig{
		PortBindings: network.PortMap{
			network.MustParsePort("2345/tcp"): []network.PortBinding{
				{
					HostIP:   netip.MustParseAddr("127.0.0.1"),
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
			Memory: memoryLimit,
		},
		AutoRemove: true,
	}

	return client.ContainerCreateOptions{
		Config:     cfg,
		HostConfig: hostCfg,
	}
}

