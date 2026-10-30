#!/bin/sh
set -eu

# 通过平台公开 API 创建知识库并上传项目部署文档，避免直接写数据库或对象存储。
for command_name in curl jq; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "$command_name is required" >&2
    exit 2
  fi
done

if [ -z "${PLATFORM_ACCESS_TOKEN:-}" ]; then
  echo "PLATFORM_ACCESS_TOKEN is required" >&2
  exit 2
fi

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
project_root=$(CDPATH= cd -- "$script_dir/../.." && pwd)
knowledge_dir=${KNOWLEDGE_DIR:-"$project_root/knowledge/docker-deployment"}
api_base=${PLATFORM_API_BASE_URL:-http://127.0.0.1/api/v1}
api_base=${api_base%/}
visibility=${KNOWLEDGE_VISIBILITY:-tenant}
knowledge_name=${KNOWLEDGE_NAME:-Gin Dify SaaS 部署文档}

case "$visibility" in
  private|tenant) ;;
  *) echo "KNOWLEDGE_VISIBILITY must be private or tenant" >&2; exit 2 ;;
esac

if [ ! -d "$knowledge_dir" ]; then
  echo "knowledge directory does not exist: $knowledge_dir" >&2
  exit 2
fi

request_body=$(jq -n --arg name "$knowledge_name" --arg visibility "$visibility" \
  '{name:$name,visibility:$visibility}')
create_response=$(curl --silent --show-error --fail-with-body \
  -X POST "$api_base/knowledge-bases" \
  -H "Authorization: Bearer $PLATFORM_ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  --data "$request_body")
knowledge_base_id=$(printf '%s' "$create_response" | jq -er \
  'if .code == 0 then .data.id else error(.message // "create knowledge base failed") end')

echo "created knowledge base: $knowledge_base_id"

document_count=0
for document in "$knowledge_dir"/*.md; do
  [ -f "$document" ] || continue
  upload_response=$(curl --silent --show-error --fail-with-body \
    -X POST "$api_base/knowledge-bases/$knowledge_base_id/documents" \
    -H "Authorization: Bearer $PLATFORM_ACCESS_TOKEN" \
    -F "file=@$document;type=text/markdown")
  indexing_status=$(printf '%s' "$upload_response" | jq -er \
    'if .code == 0 then .data.indexingStatus else error(.message // "upload failed") end')
  document_count=$((document_count + 1))
  echo "uploaded $(basename "$document"): $indexing_status"
done

if [ "$document_count" -eq 0 ]; then
  echo "no Markdown documents found in $knowledge_dir" >&2
  exit 1
fi

echo "knowledge import submitted: id=$knowledge_base_id documents=$document_count"
