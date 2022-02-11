all:
	mkdir -p target
	cd cmd; go build -o ../target/metalbond

run-server: all
	cd target && ./metalbond server --listen 0.0.0.0:1337

run-client1: all
	cd target && ./metalbond client --server 127.0.0.1:1337

run-client2: all
	cd target && ./metalbond client --server 127.0.0.1:1337

.PHONY: grpc
grpc:
	protoc -I ./proto --go_out=. --go-grpc_out=. ./proto/metalbond.proto