#!/bin/bash
tmp_dir=$(mktemp -d)

echo "Creating MetalBond Debian Package..."

make

mkdir -p $tmp_dir/metalbond/DEBIAN

mkdir -p $tmp_dir/metalbond/opt/metalbond/bin
mkdir -p $tmp_dir/metalbond/opt/metalbond/html
mkdir -p $tmp_dir/metalbond/usr/share/metalbond/systemd-units/

cp target/metalbond $tmp_dir/metalbond/opt/metalbond/bin/
cp -ra target/html $tmp_dir/metalbond/opt/metalbond/
cp deb/systemd/* $tmp_dir/metalbond/usr/share/metalbond/systemd-units/

cp deb/control $tmp_dir/metalbond/DEBIAN/
sed -i "s/METALBOND_VERSION/$METALBOND_VERSION/" $tmp_dir/metalbond/DEBIAN/control
cp deb/postinst $tmp_dir/metalbond/DEBIAN/
cp deb/preinst $tmp_dir/metalbond/DEBIAN/

( cd $tmp_dir && dpkg-deb --build metalbond )

mv $tmp_dir/metalbond.deb target/metalbond_$METALBOND_VERSION-amd64.deb

rm -rf $tmp_dir