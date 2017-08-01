package main

import (
	"context"
	"net"
	"sync"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
)

func makeFDPoller() (*epoller, error) {
	fd, err := unix.EpollCreate1(0)
	if err != nil {
		return nil, err
	}
	return &epoller{
		fd:       fd,
		chEvents: make(chan []unix.EpollEvent),
		chWait:   make(chan struct{}),
	}, nil
}

func proxyTCP(ctx context.Context, client, backend *net.TCPConn) error {
	var wg sync.WaitGroup
	wg.Add(2)
	ctx, cancel := context.WithCancel(ctx)

	done := func() {
		wg.Done()
		cancel()
	}
	if err := copyTCPConn(ctx, client, backend, done); err != nil {
		return err
	}
	if err := copyTCPConn(ctx, backend, client, done); err != nil {
		return err
	}

	wg.Wait()
	return nil
}

type epoller struct {
	fd       int
	chEvents chan []unix.EpollEvent
	chWait   chan struct{}
}

func (e *epoller) Add(fd int, flags uint32) error {
	event := &unix.EpollEvent{
		Fd:     int32(fd),
		Events: flags | unix.EPOLLHUP | unix.EPOLLERR | unix.EPOLLRDHUP | unix.EPOLLET,
	}
	if err := unix.EpollCtl(e.fd, unix.EPOLL_CTL_ADD, fd, event); err != nil {
		return err
	}

	return nil
}

func (e *epoller) Wait() <-chan []unix.EpollEvent {
	return e.chEvents
}

func (e *epoller) run(ctx context.Context) {
	var events [32]unix.EpollEvent
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		nevents, err := unix.EpollWait(e.fd, events[:], -1)
		if err != nil {
			close(e.chEvents)
			return
		}
		select {
		case e.chEvents <- events[:nevents]:
		case <-ctx.Done():
		}
	}
}

func (e *epoller) Close() {
	unix.Close(e.fd)
}

func copyTCPConn(ctx context.Context, dst, src *net.TCPConn, done func()) (err error) {
	fdst, err := dst.File()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			fdst.Close()
		}
	}()
	if err := unix.SetNonblock(int(fdst.Fd()), true); err != nil {
		return err
	}

	fsrc, err := src.File()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			fsrc.Close()
		}
	}()

	if err := unix.SetNonblock(int(fsrc.Fd()), true); err != nil {
		return err
	}

	var pipe [2]int
	if err := unix.Pipe2(pipe[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		return err
	}

	defer func() {
		if err != nil {
			unix.Close(pipe[0])
			unix.Close(pipe[1])
		}
	}()

	poller, err := makeFDPoller()
	if err != nil {
		return err
	}

	srcfd := int(fsrc.Fd())
	dstfd := int(fdst.Fd())
	pipeR := pipe[0]
	pipeW := pipe[1]

	err = poller.Add(dstfd, unix.EPOLLOUT)
	if err != nil {
		return errors.Wrap(err, "error adding dst fd to poller")
	}
	err = poller.Add(srcfd, unix.EPOLLIN)
	if err != nil {
		return errors.Wrap(err, "error adding src fd to poller")
	}

	ctx, cancel := context.WithCancel(ctx)
	go poller.run(ctx)

	go func() {
		defer done()
		copyInKernel(ctx, poller, pipe, dstfd, srcfd)
		cancel()
		src.CloseRead()
		dst.CloseWrite()

		fsrc.Close()
		fdst.Close()
		unix.Close(pipeR)
		unix.Close(pipeW)
		poller.Close()
	}()
	return nil
}

func copyInKernel(ctx context.Context, poller *epoller, pipe [2]int, dst, src int) {
	for {
		select {
		case <-ctx.Done():
			return
		case events, ok := <-poller.Wait():
			if !ok {
				return
			}

			for _, event := range events {
				if event.Events&unix.EPOLLHUP > 0 || event.Events&unix.EPOLLERR > 0 || event.Events&unix.EPOLLRDHUP > 0 {
					return
				}
			}
		default:
		}

		if err := startCopy(ctx, pipe, src, dst); err != nil {
			return
		}
	}

}

func startCopy(ctx context.Context, pipe [2]int, src, dst int) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		_, err := unix.Splice(src, nil, pipe[1], nil, 16*1024, unix.SPLICE_F_MOVE|unix.SPLICE_F_NONBLOCK)
		if err != nil {
			if err == unix.EWOULDBLOCK {
				break
			}
			return err
		}
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		_, err := unix.Splice(pipe[0], nil, dst, nil, 16*1024, unix.SPLICE_F_MOVE|unix.SPLICE_F_NONBLOCK)
		if err != nil {
			if err == unix.EWOULDBLOCK {
				break
			}
			return err
		}
	}
	return nil
}
