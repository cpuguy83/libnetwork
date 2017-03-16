//go:generate protoc -I . --gofast_out=plugins=grpc,import_path=github.com/docker/libnetwork/api/proxy,Mgoogle/protobuf/empty.proto=github.com/gogo/protobuf/types:. proxy.proto

package proxy
