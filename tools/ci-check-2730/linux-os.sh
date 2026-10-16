#!/bin/sh
set -eu
{ uname -a; cat /etc/os-release; } > /work/evidence/os.log
git rev-parse HEAD 'HEAD^{tree}' > /work/evidence/final-head-tree.log
test ! -e /var/run/docker.sock
test ! -e /dev/nvidia0
test ! -e /dev/dri
