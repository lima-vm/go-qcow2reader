#!/bin/bash
set -eu -o pipefail
if [ "$#" -ne 1 ]; then
	echo >&2 "Usage: $0 FILE"
	exit 1
fi

name_qcow2="$1"

echo "Input file: ${name_qcow2}"
set -x
go-qcow2reader-example snapshot "${name_qcow2}"
set +x

# Canonicalize qemu-img's SnapshotInfo entries.
# The qcow2-specific fields ("vm-state-size", "vm-clock-sec", "vm-clock-nsec",
# "icount") are not printed by `go-qcow2reader-example snapshot --list`,
# so they are ignored.
expected="$(qemu-img info --output=json "${name_qcow2}" |
	jq -S '[.snapshots[]? | {
		id,
		name,
		date_sec: ."date-sec",
		date_nsec: ."date-nsec"
	}]')"

if ! echo "${expected}" | jq -e 'length > 0' >/dev/null; then
	echo >&2 "Expected at least one snapshot in ${name_qcow2}"
	exit 1
fi

# Canonicalize go-qcow2reader's snapshot entries.
# "created_at" (RFC 3339, UTC) is split into seconds and nanoseconds.
got="$(go-qcow2reader-example snapshot "${name_qcow2}" |
	jq -S '[.[]? | (.created_at | capture("^(?<d>[^.]+?)(\\.(?<f>[0-9]+))?Z$")) as $t | {
		id,
		name,
		date_sec: ($t.d + "Z" | fromdateiso8601),
		date_nsec: ((($t.f // "") + "000000000")[0:9] | tonumber)
	}]')"

echo "Expected: ${expected}"
echo "Got: ${got}"
if [ "${expected}" = "${got}" ]; then
	echo "OK"
else
	echo "FAIL"
	exit 1
fi
