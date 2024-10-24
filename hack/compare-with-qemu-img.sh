#!/bin/bash
set -eu -o pipefail
if [ "$#" -ne 1 ]; then
	echo >&2 "Usage: $0 FILE"
	exit 1
fi

name_qcow2="$1"
name_raw_a="${name_qcow2}.raw_a"
name_raw_b="${name_qcow2}.raw_b"
name_raw_c="${name_qcow2}.raw_c"

echo "Input file: ${name_qcow2}"
set -x
go-qcow2reader-example info "${name_qcow2}"
set +x

echo "===== Phase 1: full read ====="
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
echo "Converting ${name_qcow2} to ${name_raw_b} with go-qcow2reader read"
set -x
go-qcow2reader-example read "${name_qcow2}" >"${name_raw_b}"
sha256sum "${name_raw_b}" | tee "${name_raw_b}.sha256"
set +x

rm -f "${name_raw_c}" "${name_raw_c}.sha256"
echo "Converting ${name_qcow2} to ${name_raw_c} with go-qcow2reader convert"
set -x
go-qcow2reader-example convert "${name_qcow2}" "${name_raw_c}"
sha256sum "${name_raw_c}" | tee "${name_raw_c}.sha256"
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

got="$(cut -d " " -f 1 <"${name_raw_c}.sha256")"
echo "Comparing: ${expected} vs ${got}"
if [ "${expected}" = "${got}" ]; then
	echo "OK"
else
	echo "FAIL"
	set -x
	qemu-img compare "${name_raw_a}" "${name_raw_c}"
	exit 1
fi

echo "===== Phase 2: random read ====="
for offset in 1 22 333 4444 55555 666666 7777777 88888888; do
	for length in 1 22 333 4444 55555 666666 7777777 88888888; do
		set -x
		set +o pipefail
		expected="$(tail -c "+$((${offset} + 1))" "${name_raw_a}" | head -c "${length}" | sha256sum - | cut -d " " -f 1)"
		set -o pipefail
		got="$(go-qcow2reader-example read -offset="${offset}" -length="${length}" "${name_qcow2}" | sha256sum - | cut -d " " -f 1)"
		set +x
		echo "Comparing: ${expected} vs ${got}"
		if [ "${expected}" = "${got}" ]; then
			echo "OK"
		else
			echo "FAIL"
			exit 1
		fi
	done
done

echo "===== Cleaning up... ====="
set -x
rm -f "${name_raw_b}" "${name_raw_b}.sha256" "${name_raw_c}" "${name_raw_c}.sha256"
set +x
