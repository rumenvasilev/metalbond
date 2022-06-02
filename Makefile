all:
	mkdir -p target
	rm -rf target/html && cp -ra html target
	cd cmd && go build -o ../target/metalbond

run-server: all
	cd target && ./metalbond server \
		--listen [::]:4711 \
		--http [::]:4712 \
		--keepalive 3

run-client1: all
	cd target && sudo ./metalbond client \
		--server [::1]:4711 \
		--keepalive 2 \
		--subscribe 23 \
		--announce 23#2001:db8:1::/48#2001:db8::cafe \
		--announce 23#192.168.0.0/16#2001:db8::cafe \
		--install-routes 23#100 \
		--tun overlay-tun

run-client2: all
	cd target && ./metalbond client \
		--server [::1]:4711 \
		--keepalive 2 \
		--subscribe 23 \
		--subscribe 42 \
		--announce 23#2001:db8:1::/48#2001:db8::cafe \
		--announce 23#2001:db8:2::/48#2001:db8::2:beef \
		--announce 23#2001:db8:3::/48#2001:db8::3:beef \
		--announce 23#2001:db8:4::/48#2001:db8::4:beef \
		--announce 23#2001:db8:4::/48#2001:db8::4a:beef \
		--announce 23#2001:db8:4::/48#2001:db8::4b:beef \
		--announce 23#2001:db8:4::/48#2001:db8::4c:beef \
		--announce 42#10.0.0.0/8#2001:db8::beef

.PHONY: proto
proto:
	protoc -I ./pb --go_out=. ./pb/metalbond.proto

clean:
	rm -rf target