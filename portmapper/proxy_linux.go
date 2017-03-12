package portmapper

import (
	"os"
	"os/exec"
	"syscall"
)

func newProxyCommand(proxyPath, sockPath string) (*proxyCommand, error) {
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
			Path:   path,
			Stdout: os.Stdout,
			Stderr: os.Stderr,
			Args:   []string{path, sockPath},
			SysProcAttr: &syscall.SysProcAttr{
				Pdeathsig: syscall.SIGTERM, // send a sigterm to the proxy if the daemon process dies
			},
		},
	}, nil
}
