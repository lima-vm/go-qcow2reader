#!/bin/bash
set -eu -o pipefail
if [ "$#" -ne 1 ]; then
	echo >&2 "Usage: $0 FILE"
	exit 1
fi

name_qcow2="$1"
name_raw_a="${name_qcow2}.raw_a"
name_raw_b="${name_qcow2}.raw_b"

echo "Input file: ${name_qcow2}"
set -x
go-qcow2reader-example -info "${name_qcow2}"
set +x

if [ ! -e "${name_raw_a}" ]; then
	echo "Converting ${name_qcow2} to ${name_raw_a} with qemu-img"
	set -x
	qemu-img convert -O raw "${name_qcow2}" "${name_raw_a}"
	set +x
fi

if [ ! -e "${name_raw_a}".sha256 ]; then
	set -x
	sha256sum "${name_raw_a}" | tee "${name_raw_a}.sha256"
	set +x
fi

rm -f "${name_raw_b}" "${name_raw_b}.sha256"
echo "Converting ${name_qcow2} to ${name_raw_b} with go-qcow2reader"
set -x
go-qcow2reader-example "${name_qcow2}" >"${name_raw_b}"
sha256sum "${name_raw_b}" | tee "${name_raw_b}.sha256"
set +x

expected="$(cut -d " " -f 1 <"${name_raw_a}.sha256")"
got="$(cut -d " " -f 1 <"${name_raw_b}.sha256")"
echo "Comparing: ${expected} vs ${got}"
if [ "${expected}" = "${got}" ]; then
	echo "OK"
else
	echo "FAIL"
	set -x
	qemu-img compare "${name_raw_a}" "${name_raw_b}"
	exit 1
fi

echo "Cleaning up..."
set -x
rm -f "${name_raw_b}" "${name_raw_b}.sha256"
set +x
