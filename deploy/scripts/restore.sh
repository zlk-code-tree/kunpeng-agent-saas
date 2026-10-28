#!/bin/sh
set -eu

# 恢复会覆盖数据库和对象存储，必须设置固定确认变量，降低误操作风险。
if [ "${CONFIRM_RESTORE:-}" != "gin-dify-saas" ]; then
  echo "set CONFIRM_RESTORE=gin-dify-saas after confirming the target environment" >&2
  exit 2
fi
if [ "$#" -ne 1 ]; then
  echo "usage: $0 /absolute/path/to/backup-directory" >&2
  exit 2
fi

backup_dir=$1
case "$backup_dir" in
  /*) ;;
  *) echo "backup directory must be an absolute path" >&2; exit 2 ;;
esac

cd "$backup_dir"
# 任何归档校验失败都立即退出，不进入后续破坏性恢复步骤。
shasum -a 256 -c SHA256SUMS
cd - >/dev/null

docker compose -f deploy/compose.yaml exec -T postgres \
  pg_restore -U gin_dify -d gin_dify --clean --if-exists < "$backup_dir/postgres.dump"
# 数据库恢复成功后清空对象存储卷，再恢复与该数据库快照匹配的文件归档。
docker run --rm -v deploy_objectstore_data:/target -v "$backup_dir:/backup:ro" alpine:3.22 \
  sh -c 'find /target -mindepth 1 -maxdepth 1 -exec rm -rf -- {} + && tar -C /target -xzf /backup/objectstore.tar.gz'
