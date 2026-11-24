#!/usr/bin/env bash
# Healthy while every repository in RUNNER_REPOS has a runner listening.
expected=$(cat /run/github-runner/expected 2>/dev/null) || exit 1
running=$(pgrep -fc 'Runner.Listener run')
[[ ${running:-0} -ge $expected ]]
