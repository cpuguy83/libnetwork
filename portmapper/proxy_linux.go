package portmapper

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/Sirupsen/logrus"
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
			Args:   []string{path, "serve", "--sock", sockPath, "--log-level", logrus.StandardLogger().Level.String()},
			SysProcAttr: &syscall.SysProcAttr{
				Pdeathsig: syscall.SIGTERM, // send a sigterm to the proxy if the daemon process dies
			},
		},
	}, nil
}
