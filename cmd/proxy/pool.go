package main

import (
	"io"
	"sync"
)

var bufPool = &sync.Pool{New: func() interface{} { return make([]byte, 32*1024) }}

func copyWithPool(dst io.Writer, src io.Reader) (int64, error) {
	buf := bufPool.Get().([]byte)
	n, err := io.CopyBuffer(dst, src, buf)
	bufPool.Put(buf)
	return n, err
}
