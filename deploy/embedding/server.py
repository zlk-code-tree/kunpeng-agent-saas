"""本地中文向量模型服务，仅供知识库网关在 Docker 内网调用。"""

import hashlib
import json
import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.request import urlopen

import numpy as np
import onnxruntime as ort
from tokenizers import Tokenizer


MODEL_BASE = "https://modelscope.cn/models/lyf219/bge-small-zh-v1.5-onnx/resolve/master"
FILES = {
    "bge-small-zh-v1.5.onnx": "e256a606233996478f0eb6c99c9814e3797043d4a5563b4646b10929546196e5",
    "tokenizer.json": "48cea5d44424912a6fd1ea647bf4fe50b55ab8b1e5879c3275f80e339e8fae26",
}
QUERY_PREFIX = "为这个句子生成表示以用于检索相关文章："
MODEL_DIR = Path(os.environ.get("MODEL_DIR", "/models"))


def checked_download(filename: str, expected_sha256: str) -> Path:
    """模型固定到已核验的 SHA256；下载中断时保留旧的有效文件。"""
    MODEL_DIR.mkdir(parents=True, exist_ok=True)
    destination = MODEL_DIR / filename
    if destination.exists() and hashlib.sha256(destination.read_bytes()).hexdigest() == expected_sha256:
        return destination
    temporary = destination.with_suffix(destination.suffix + ".tmp")
    digest = hashlib.sha256()
    with urlopen(f"{MODEL_BASE}/{filename}", timeout=120) as response, temporary.open("wb") as output:
        while block := response.read(1024 * 1024):
            output.write(block)
            digest.update(block)
    if digest.hexdigest() != expected_sha256:
        temporary.unlink(missing_ok=True)
        raise RuntimeError(f"model checksum mismatch: {filename}")
    temporary.replace(destination)
    return destination


model_path = checked_download("bge-small-zh-v1.5.onnx", FILES["bge-small-zh-v1.5.onnx"])
tokenizer_path = checked_download("tokenizer.json", FILES["tokenizer.json"])
tokenizer = Tokenizer.from_file(str(tokenizer_path))
tokenizer.enable_truncation(max_length=512)
tokenizer.enable_padding(pad_id=0, pad_token="[PAD]")
options = ort.SessionOptions()
options.intra_op_num_threads = 2
session = ort.InferenceSession(str(model_path), sess_options=options, providers=["CPUExecutionProvider"])
input_names = {value.name for value in session.get_inputs()}


def embed(texts: list[str], query: bool) -> list[list[float]]:
    """BGE 使用 CLS 池化与 L2 归一化；查询向量按模型约定加检索提示词。"""
    if query:
        texts = [QUERY_PREFIX + text for text in texts]
    encodings = tokenizer.encode_batch(texts)
    feeds = {
        "input_ids": np.asarray([item.ids for item in encodings], dtype=np.int64),
        "attention_mask": np.asarray([item.attention_mask for item in encodings], dtype=np.int64),
        "token_type_ids": np.asarray([item.type_ids for item in encodings], dtype=np.int64),
    }
    vectors = session.run(None, {name: feeds[name] for name in input_names})[0]
    if vectors.ndim == 3:
        vectors = vectors[:, 0, :]
    vectors = vectors / np.maximum(np.linalg.norm(vectors, axis=1, keepdims=True), 1e-12)
    return vectors.astype(np.float32).tolist()


class Handler(BaseHTTPRequestHandler):
    """限制输入批量和长度，避免检索请求占满本地推理服务。"""

    def do_GET(self):
        if self.path != "/healthz":
            self.send_error(404)
            return
        self.respond(200, {"status": "ok"})

    def do_POST(self):
        if self.path != "/embed":
            self.send_error(404)
            return
        try:
            size = int(self.headers.get("Content-Length", "0"))
            if size <= 0 or size > 128 * 1024:
                raise ValueError("invalid request size")
            payload = json.loads(self.rfile.read(size))
            texts = payload.get("texts")
            if not isinstance(texts, list) or not 1 <= len(texts) <= 8:
                raise ValueError("texts must contain 1-8 items")
            if any(not isinstance(text, str) or not text.strip() or len(text) > 5000 for text in texts):
                raise ValueError("invalid text")
            self.respond(200, {"vectors": embed(texts, bool(payload.get("query", False)))})
        except (ValueError, json.JSONDecodeError) as error:
            self.respond(400, {"error": str(error)})
        except Exception:
            self.respond(500, {"error": "embedding failed"})

    def respond(self, status: int, payload: dict):
        data = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", 8000), Handler).serve_forever()
