## ============================================================
## LLM Logger Query API - 多阶段构建
## ============================================================

FROM python:3.12-slim AS builder

ENV PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PYTHONDONTWRITEBYTECODE=1

WORKDIR /app

# 用 uv 加速依赖安装
RUN pip install --no-cache-dir uv

COPY pyproject.toml ./
COPY app ./app

# 编译/安装依赖到 venv
RUN uv venv /opt/venv && \
    uv pip install --python /opt/venv/bin/python --no-cache .

## ------- runtime -------
FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PATH="/opt/venv/bin:$PATH"

WORKDIR /app

COPY --from=builder /opt/venv /opt/venv
COPY app ./app

EXPOSE 8000

# 非 root 运行
RUN useradd -m -u 1000 appuser && chown -R appuser:appuser /app
USER appuser

CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8000", "--proxy-headers"]
