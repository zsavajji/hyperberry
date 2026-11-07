#!/bin/sh
# Copies build/hyperberryd (or the file given as $1, e.g. build/mlowbench) to the Q10 over ssh,
# makes it runnable, prints its path there.
# BB10 runs a copied binary only once `/proc/boot/pathtrust !<file>` trusts that one file, and
# a file it refused stays refused, even as a new file that reuses its inode. So each build
# gets a fresh name, and a refused copy is emptied and parked in denied/ (kept, so its inode is
# never handed out again) before the next try. Older builds that did run are removed; a file
# other than the engine is named <basename>-<sha8>-<n>, so it only replaces its own older copies.
set -eu
ROOT=$(cd "$(dirname "$0")/.." && pwd)
PHONE=${PHONE:-bb10rootx}
DIR=/root/hyperberry
SRC=${1:-$ROOT/build/hyperberryd}
base=$(basename "$SRC")
[ "$base" = hyperberryd ] && base=hbd
name=$base-$(sha256sum "$SRC" | cut -c1-8)
scp -q -o BatchMode=yes "$SRC" "$PHONE:/dev/shmem/$name"
ssh -o BatchMode=yes "$PHONE" "mkdir -p $DIR/denied && cd $DIR && for n in 1 2 3 4 5 6; do
	f=$name-\$n
	# A copy of this build that already runs is reused: copying over it (it may be running)
	# would make cp stop and ask.
	[ -e \$f ] || { cp /dev/shmem/$name \$f && chmod +x \$f && /proc/boot/pathtrust !$DIR/\$f; }
	if ./\$f -h >/dev/null 2>&1; then
		for o in $base-*; do [ \$o = \$f ] || rm -f \$o; done
		rm -f /dev/shmem/$name; echo $DIR/\$f; exit 0
	fi
	: > \$f; mv \$f denied/
done
rm -f /dev/shmem/$name; echo 'no copy would run' >&2; exit 1"
