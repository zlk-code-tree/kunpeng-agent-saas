"""生成本地知识库部署凭据；不会覆盖已存在的配置。"""

from pathlib import Path
import secrets

target = Path(__file__).resolve().parents[1] / ".env.knowledge.local"
if target.exists():
    print(f"existing credentials: {target}")
else:
    target.write_text(
        "KNOWLEDGE_DB_PASSWORD=" + secrets.token_hex(24) + "\n"
        "KNOWLEDGE_API_KEY=" + secrets.token_urlsafe(48) + "\n",
        encoding="utf-8",
    )
    target.chmod(0o600)
    print(f"created credentials: {target}")
