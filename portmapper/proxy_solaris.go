package portmapper

import (
	"os"
	"os/exec"
	"syscall"
)

func newProxyCommand(proxyPath, sockPath string) (userlandProxy, error) {
	path := proxyPath
	if proxyPath == "" {
		cmd, err := exec.LookPath(userlandProxyCommandName)
		if err != nil {
			return nil, err
		}
		path = cmd
	}
	syscall.Unlink(sockPath)

	return &proxyCommand{
		sockPath: sockPath,
		cmd: &exec.Cmd{
			Stdout: os.Stdout,
			Stderr: os.Stderr,
			Path:   path,
			Args:   []string{path, sockPath},
		},
	}, nil
}
