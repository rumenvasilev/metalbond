all:
	mkdir -p target
	cd cmd && go build -o ../target/metalbond

run-server: all
	cd target && ./metalbond server --listen [::]:1337 \
		--node-uuid 4c85bf91-b45e-4e64-99d7-0ae2c89af502 \
		--hostname fra4-router-1

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

.PHONY: proto
proto:
	protoc -I ./pb --go_out=. ./pb/metalbond.proto