all:
	mkdir -p target
	cd cmd && go build -o ../target/metalbond

run-server: all
	cd target && ./metalbond server -v \
		--listen [::]:4711 \
		--node-uuid 4c85bf91-b45e-4e64-99d7-0ae2c89af502 \
		--hostname server-1 \
		--keepalive 10

run-client1: all
	cd target && ./metalbond client -v \
		--server [::1]:4711 \
		--node-uuid 9517e08c-454e-49bc-8cad-f82db1b4e067 \
		--hostname client-1 \
		--keepalive 5 \
		--subscribe 23 \
		--announce 23#2001:db8:1::/48 \
		--announce 23#192.168.0.0/16

run-client2: all
	cd target && ./metalbond client -v \
		--server [::1]:4711 \
		--node-uuid e461e9b1-40e2-4246-870e-2836ca961fe7 \
		--hostname client-2 \
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