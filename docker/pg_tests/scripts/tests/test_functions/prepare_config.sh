#!/bin/sh

# Writes recovery configuration in a version-appropriate way.
# PG 12 removed recovery.conf: settings go into postgresql.conf and recovery
# mode is signaled by a .signal file (recovery.signal for archive recovery,
# standby.signal for standby mode). Reads the config lines from stdin.
write_recovery_conf() {
  WRC_DATA_DIR="${1:-${PGDATA}}"
  WRC_VERSION=$(cat "${WRC_DATA_DIR}/PG_VERSION")
  WRC_CONTENT=$(cat)
  if awk 'BEGIN {exit !('"$WRC_VERSION"' >= 12)}'; then
    if echo "${WRC_CONTENT}" | grep -q "standby_mode"; then
      touch "${WRC_DATA_DIR}/standby.signal"
      echo "${WRC_CONTENT}" | grep -v "standby_mode" | grep -v "trigger_file" >> "${WRC_DATA_DIR}/postgresql.conf"
    else
      touch "${WRC_DATA_DIR}/recovery.signal"
      echo "${WRC_CONTENT}" >> "${WRC_DATA_DIR}/postgresql.conf"
    fi
  else
    echo "${WRC_CONTENT}" > "${WRC_DATA_DIR}/recovery.conf"
  fi
}

prepare_config() {
  if [ -z "${1}" ]; then
    echo "prepare_config should be run with test specific config file argument"
    exit 1
  fi

  CONFIG_FILE=$1
  COMMON_CONFIG="/tmp/configs/common_config.json"
  TMP_CONFIG="/tmp/configs/tmp_config.json"
  cat "${CONFIG_FILE}" > "${TMP_CONFIG}"
  echo "," >> "${TMP_CONFIG}"
  cat "${COMMON_CONFIG}" >> "${TMP_CONFIG}"
  /tmp/scripts/wrap_config_file.sh "${TMP_CONFIG}"
}