//go:generate protoc -I . --gofast_out=plugins=grpc,import_path=github.com/docker/libnetwork/cmd/proxy/rpc,Mgoogle/protobuf/empty.proto=github.com/gogo/protobuf/types:. proxy.proto

package rpc
