all:
	mkdir -p target
	cd cmd; go build -o ../target/metalbond

run-server: all
	cd target && ./metalbond server --listen [::]:1337

run-client1: all
	cd target && ./metalbond client --server [::1]:1337 \
		--subscribe 23 \
		--announce 23#2001:db8:1::/48 \
		--announce 23#192.168.0.0/16

run-client2: all
	cd target && ./metalbond client --server [::1]:1337 \
		--subscribe 23 \
		--subscribe 42 \
		--announce 23#2001:db8:2::/48 \
		--announce 42#10.0.0.0/8

.PHONY: grpc
grpc:
	protoc -I ./proto --go_out=. --go-grpc_out=. ./proto/metalbond.proto