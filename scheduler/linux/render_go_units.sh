#!/usr/bin/env bash
set -euo pipefail
root=$1
binary=$2
directory=${3:-"$HOME/.config/systemd/user"}
mkdir -p -- "$directory"

quote_unit() {
  local value=$1
  value=${value//\\/\\\\}
  value=${value//\"/\\\"}
  value=${value//%/%%}
  value=${value//\$/\$\$}
  printf '"%s"' "$value"
}

working_directory=${root//%/%%}
for command in collect generate recover; do
  action=$command
  if [[ "$command" == generate ]]; then
    action=run
  fi
  cat > "$directory/daily-log-$command.service" <<UNIT
[Unit]
Description=Personal daily log $command
After=network-online.target
StartLimitIntervalSec=0

[Service]
Type=oneshot
WorkingDirectory=$working_directory
ExecStart=$(quote_unit "$binary") --config $(quote_unit "$root/config.yaml") $action
Restart=on-failure
RestartSec=15min
TimeoutStartSec=1h
UMask=0077

[Install]
WantedBy=default.target
UNIT
done

cat > "$directory/daily-log-generate.timer" <<'UNIT'
[Unit]
Description=Daily log generate at 23:00:00 Asia/Shanghai

[Timer]
OnCalendar=*-*-* 23:00:00 Asia/Shanghai
Persistent=true
AccuracySec=1s
Unit=daily-log-generate.service

[Install]
WantedBy=timers.target
UNIT
