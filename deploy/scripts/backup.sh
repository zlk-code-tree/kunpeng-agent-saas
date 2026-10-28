#!/bin/sh
set -eu

# 备份目录必须由运维人员显式传入绝对路径，避免相对路径随执行目录变化。
if [ "$#" -ne 1 ]; then
  echo "usage: $0 /absolute/backup/directory" >&2
  exit 2
fi

backup_dir=$1
case "$backup_dir" in
  /*) ;;
  *) echo "backup directory must be an absolute path" >&2; exit 2 ;;
esac

timestamp=$(date -u +%Y%m%dT%H%M%SZ)
target_dir="$backup_dir/gin-dify-saas-$timestamp"
mkdir -p "$target_dir"

# 数据库使用 PostgreSQL 自定义格式，便于恢复时按对象处理；对象存储卷单独压缩归档。
docker compose -f deploy/compose.yaml exec -T postgres \
  pg_dump -U gin_dify -d gin_dify --format=custom > "$target_dir/postgres.dump"
docker run --rm -v deploy_objectstore_data:/source:ro -v "$target_dir:/backup" alpine:3.22 \
  tar -C /source -czf /backup/objectstore.tar.gz .

# 对两个归档生成校验清单，恢复脚本会在执行任何覆盖操作前先校验完整性。
shasum -a 256 "$target_dir/postgres.dump" "$target_dir/objectstore.tar.gz" > "$target_dir/SHA256SUMS"
echo "$target_dir"
