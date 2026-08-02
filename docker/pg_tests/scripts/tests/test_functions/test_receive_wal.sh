#!/bin/sh
set -e
test_receive_wal()
{
  TMP_CONFIG=$1
  initdb ${PGDATA}

  pg_ctl -D ${PGDATA} -w start

  wal-g --config=${TMP_CONFIG} wal-receive &

  # Wait for the replication slot to be created before generating WAL,
  # otherwise wal-receive may start streaming from a later segment and
  # never upload the first one, failing wal-verify integrity.
  SLOT_WAIT_ATTEMPTS=150
  until psql -tAc "SELECT 1 FROM pg_replication_slots WHERE slot_name='walg'" | grep -q 1; do
    SLOT_WAIT_ATTEMPTS=$((SLOT_WAIT_ATTEMPTS - 1))
    if [ "${SLOT_WAIT_ATTEMPTS}" -eq 0 ]; then
      echo "Replication slot 'walg' was not created in time"
      return 1
    fi
    sleep 0.2
  done

  pgbench -i -s 5 postgres
  pg_dumpall -f /tmp/dump1
  pgbench -c 2 -T 10 -S
  sleep 1
  VERIFY_OUTPUT=$(mktemp)
  # Verify and store in temp file
  wal-g --config=${TMP_CONFIG} wal-verify integrity > "${VERIFY_OUTPUT}"
  pg_ctl -D ${PGDATA} -w stop -m immediate

  # parse verify results
  VERIFY_RESULT=$(awk 'BEGIN{FS=":"}$1~/integrity check status/{print $2}' $VERIFY_OUTPUT)

  cat "${VERIFY_OUTPUT}"

  # check verify results to end with 'OK'
  if echo "$VERIFY_RESULT" | grep -qP "\bOK$"; then
    /tmp/scripts/drop_pg.sh
    rm ${TMP_CONFIG}
    echo "WAL receive success!!!!!!"
    return 0
  fi
  echo "WAL not received as expected!!!!!"
  return 1
}
