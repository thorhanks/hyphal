#!/bin/sh
# P0-05 items 4–7: commit latency, LoadAll, cross-compilation, binary size.
# Set DRIVERSPIKE_DIR to a directory on a real disk for fsync-honest numbers.
set -eu
cd "$(dirname "$0")"
out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT

echo "## go test (behavior)"
go test -count=1 -v -run 'Test[^C]' ./... | grep -E '^(---|ok|FAIL|PASS)|busy after|backup during'

echo "## commit latency"
go test -count=1 -v -run TestCommitLatency ./... | grep RESULT

echo "## benchmarks"
go test -run '^$' -bench . -benchmem -count=3 ./... | grep '^Benchmark'

echo "## cross-compile and size (CGO_ENABLED=0, -trimpath -ldflags='-s -w')"
for target in linux/amd64 linux/arm64 darwin/arm64 darwin/amd64; do
	for d in none modernc ncruces; do
		bin="$out/$d-$(echo "$target" | tr / -)"
		if CGO_ENABLED=0 GOOS=${target%/*} GOARCH=${target#*/} go build -trimpath -ldflags='-s -w' -o "$bin" "./cmd/$d"; then
			printf '%-13s %-8s %10d bytes\n' "$target" "$d" "$(wc -c <"$bin")"
		else
			printf '%-13s %-8s FAILED\n' "$target" "$d"
		fi
	done
done
