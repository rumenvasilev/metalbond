all:
	mkdir -p target
	cd cmd && go build -o ../target/metalbond

run-server: all
	cd target && ./metalbond server -v \
		--listen [::]:4711 \
		--keepalive 10

run-client1: all
	cd target && sudo ./metalbond client -v \
		--install-routes \
		--link ip6tnl0 \
		--server [::1]:4711 \
		--keepalive 5 \
		--subscribe 23 \
		--announce 23#2001:db8:1::/48#2001:db8::cafe \
		--announce 23#192.168.0.0/16#2001:db8::cafe

run-client2: all
	cd target && ./metalbond client -v \
		--server [::1]:4711 \
		--keepalive 5 \
		--subscribe 23 \
		--subscribe 42 \
		--announce 23#2001:db8:2::/48 \
		--announce 42#10.0.0.0/8

.PHONY: proto
proto:
	protoc -I ./pb --go_out=. ./pb/metalbond.proto

clean:
	rm -rf target