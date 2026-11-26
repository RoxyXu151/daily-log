#!/usr/bin/env bash
set -euo pipefail
for unit in daily-log-collect.timer daily-log-generate.timer daily-log-collect.service daily-log-generate.service daily-log-recover.service; do
  systemctl --user disable --now "$unit" 2>/dev/null || true
  rm -f -- "$HOME/.config/systemd/user/$unit"
done
systemctl --user daemon-reload
echo '定时任务已卸载，项目、凭证和数据库保留。'
