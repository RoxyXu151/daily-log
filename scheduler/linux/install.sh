#!/usr/bin/env bash
set -euo pipefail
PROJECT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
DAILY_LOG_BIN="${DAILY_LOG_BIN:-$PROJECT_DIR/dist/daily-log}"
if [[ ! -x "$DAILY_LOG_BIN" ]]; then
  echo "先构建 dist/daily-log，或设置 DAILY_LOG_BIN 为可执行文件路径" >&2
  exit 1
fi
DAILY_LOG_BIN="$(realpath -- "$DAILY_LOG_BIN")"
bash "$PROJECT_DIR/scheduler/linux/render_go_units.sh" "$PROJECT_DIR" "$DAILY_LOG_BIN"
# Migrate the old two-timer schedule to one ordered nightly pipeline.
if [[ -f "$HOME/.config/systemd/user/daily-log-collect.timer" ]]; then
  systemctl --user disable --now daily-log-collect.timer
  rm -- "$HOME/.config/systemd/user/daily-log-collect.timer"
fi
systemctl --user daemon-reload
systemctl --user enable --now daily-log-generate.timer daily-log-recover.service
systemctl --user restart daily-log-generate.timer
systemctl --user list-timers 'daily-log-*' --no-pager
echo '已安装：每天北京时间 23:00 依次采集、总结当天日志并同步；关机错过后补跑。'
