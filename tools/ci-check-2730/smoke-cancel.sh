#!/bin/sh
set -eu
sleep 300 &
child=$!
kill -0 "$child"
printf 'cancel_child_observed=true pid=%s\n' "$child"
wait "$child"
