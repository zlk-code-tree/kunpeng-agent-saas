"""从本机环境文件生成账号清单的明文凭据段；不向终端输出秘密。"""

from __future__ import annotations

import html
import os
from pathlib import Path
import re
import tempfile


ROOT = Path(__file__).resolve().parents[2]
DOCUMENT = ROOT / "deploy" / "本地服务账号与端口.md"
BEGIN = "<!-- BEGIN LOCAL CREDENTIALS -->"
END = "<!-- END LOCAL CREDENTIALS -->"


def env_value(path: Path, key: str) -> str:
    """读取第一处精确匹配的配置项，保留值中的等号和特殊字符。"""
    for line in path.read_text(encoding="utf-8").splitlines():
        if line.lstrip().startswith("#") or "=" not in line:
            continue
        name, value = line.split("=", 1)
        if name.strip() == key:
            return value.strip().strip('"\'')
    raise RuntimeError(f"missing {key} in {path}")


def yaml_scalar(path: Path, key: str) -> str:
    """只提取本地 Compose 中的静态开发凭据，不解析变量替换表达式。"""
    pattern = re.compile(rf"^\s*{re.escape(key)}:\s*(\S+)\s*$", re.MULTILINE)
    match = pattern.search(path.read_text(encoding="utf-8"))
    if not match or match.group(1).startswith("${"):
        raise RuntimeError(f"missing static {key} in {path}")
    return match.group(1).strip('"\'')


def cell(value: str) -> str:
    return f"<code>{html.escape(value).replace('|', '&#124;')}</code>"


def main() -> None:
    admin_password = os.environ.get("PLATFORM_ADMIN_PASSWORD")
    if not admin_password:
        raise RuntimeError("PLATFORM_ADMIN_PASSWORD is required")

    app_compose = ROOT / "deploy" / "compose.yaml"
    knowledge_env = ROOT / "deploy" / ".env.knowledge.local"
    mysql_env = Path("/Users/zhanglikun/workspace/service/docker/mysql/8/.env")
    redis_env = Path("/Users/zhanglikun/workspace/service/docker/redis/6/.env")
    dify_env = Path("/Users/zhanglikun/workspace/service/dify/docker/.env")

    rows = [
        ("Gin SaaS 后台", "admin", admin_password, "用户提供；当前未验证登录"),
        ("业务 PostgreSQL", "gin_dify", yaml_scalar(app_compose, "POSTGRES_PASSWORD"), "deploy/compose.yaml"),
        ("平台 Redis 8", "无用户", "无密码", "deploy/compose.yaml"),
        ("S3 对象存储", yaml_scalar(app_compose, "AWS_ACCESS_KEY_ID"), yaml_scalar(app_compose, "AWS_SECRET_ACCESS_KEY"), "deploy/compose.yaml"),
        ("自管 pgvector", "knowledge", env_value(knowledge_env, "KNOWLEDGE_DB_PASSWORD"), "deploy/.env.knowledge.local"),
        ("外部知识库 API", "Bearer API Key", env_value(knowledge_env, "KNOWLEDGE_API_KEY"), "deploy/.env.knowledge.local"),
        ("MySQL 8 root", "root", env_value(mysql_env, "MYSQL_ROOT_PASSWORD"), "service/docker/mysql/8/.env"),
        ("MySQL 8 普通用户", env_value(mysql_env, "MYSQL_USER"), env_value(mysql_env, "MYSQL_PASSWORD"), "service/docker/mysql/8/.env"),
        ("独立 Redis 6", "无用户", env_value(redis_env, "PASSWORD"), "service/docker/redis/6/.env"),
        ("Dify PostgreSQL", env_value(dify_env, "DB_USERNAME"), env_value(dify_env, "DB_PASSWORD"), "service/dify/docker/.env"),
        ("Dify 向量库", env_value(dify_env, "PGVECTOR_USER"), env_value(dify_env, "PGVECTOR_PASSWORD"), "service/dify/docker/.env"),
        ("Dify Redis", "无用户", env_value(dify_env, "REDIS_PASSWORD"), "service/dify/docker/.env"),
        ("Dify 管理员", "初始化账号", "无法从现有配置确认", "请在 Dify 登录界面验证或重置"),
    ]
    table = "| 服务 | 用户名或认证方式 | 密码或密钥 | 来源 |\n| --- | --- | --- | --- |\n"
    table += "\n".join(
        f"| {name} | {cell(user)} | {cell(secret)} | {source} |"
        for name, user, secret, source in rows
    )
    text = DOCUMENT.read_text(encoding="utf-8")
    if text.count(BEGIN) != 1 or text.count(END) != 1:
        raise RuntimeError("credential markers missing or duplicated")
    before, remaining = text.split(BEGIN, 1)
    _, after = remaining.split(END, 1)
    output = before + BEGIN + "\n" + table + "\n" + END + after

    # 先以 0600 权限写入同目录临时文件，再原子替换，避免短暂出现可被其他用户读取的内容。
    with tempfile.NamedTemporaryFile("w", encoding="utf-8", dir=DOCUMENT.parent, prefix=".credentials-", delete=False) as temp:
        os.fchmod(temp.fileno(), 0o600)
        temp.write(output)
        temporary = Path(temp.name)
    temporary.replace(DOCUMENT)
    print(f"updated local document with {len(rows)} credential entries")


if __name__ == "__main__":
    main()
