#!/bin/sh
set -eu
sleep 300 &
child=$!
kill -0 "$child"
printf 'normal_child_observed=true pid=%s\n' "$child"
