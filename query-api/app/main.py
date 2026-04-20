"""FastAPI 应用入口."""

from __future__ import annotations

import logging
from contextlib import asynccontextmanager

from fastapi import FastAPI, Request
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import JSONResponse

from app import __version__
from app.config import get_settings
from app.database.clickhouse import get_ch_client
from app.routers import errors as errors_router
from app.routers import health as health_router
from app.routers import search as search_router
from app.routers import stats as stats_router
from app.routers import traces as traces_router
from app.routers import users as users_router


def setup_logging(level: str) -> None:
    logging.basicConfig(
        level=level.upper(),
        format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
        datefmt="%Y-%m-%d %H:%M:%S",
    )
    # 抑制噪音
    logging.getLogger("aioboto3").setLevel(logging.WARNING)
    logging.getLogger("aiobotocore").setLevel(logging.WARNING)
    logging.getLogger("botocore").setLevel(logging.WARNING)


@asynccontextmanager
async def lifespan(_: FastAPI):
    settings = get_settings()
    setup_logging(settings.log_level)
    log = logging.getLogger(__name__)
    log.info("LLM Logger Query API v%s 启动", __version__)

    # 预热 CH client (验证可达). 容忍首次失败: CH 首次启动时 initdb.d
    # 可能还在跑建表/建索引, 此时 8123 可能拒绝连接. 启动阶段失败不阻塞服务,
    # 后续请求会延迟重新 init client (见 database/clickhouse.py).
    ch = None
    try:
        ch = await get_ch_client()
        if not await ch.ping():
            log.warning("ClickHouse 不可达 - 仍启动 service, 请求会延迟重试")
    except Exception as e:
        log.warning("ClickHouse 初始化失败,服务继续启动: %s", e)

    yield

    log.info("Query API 关闭中...")
    if ch is not None:
        await ch.close()


def create_app() -> FastAPI:
    settings = get_settings()
    app = FastAPI(
        title="LLM Logger Query API",
        version=__version__,
        description="访问 ClickHouse 中采集的 LLM 调用元数据 + COS 原始 body",
        lifespan=lifespan,
    )

    app.add_middleware(
        CORSMiddleware,
        allow_origins=settings.cors_origins_list,
        allow_methods=["*"],
        allow_headers=["*"],
    )

    # ---- 全局异常处理 ----
    _log = logging.getLogger(__name__)

    @app.exception_handler(Exception)
    async def _unhandled_exception_handler(request: Request, exc: Exception):
        exc_name = type(exc).__name__
        # ClickHouse 连接 / 查询异常 → 503
        ch_errors = ("NetworkError", "SocketError", "DatabaseError", "OperationalError")
        if exc_name in ch_errors or "clickhouse" in type(exc).__module__.lower():
            _log.error("ClickHouse 异常: %s: %s", exc_name, exc)
            return JSONResponse(
                status_code=503,
                content={"detail": f"ClickHouse 不可用: {exc_name}: {exc}"},
            )
        _log.exception("Unhandled exception on %s %s", request.method, request.url.path)
        return JSONResponse(
            status_code=500,
            content={"detail": f"Internal Server Error: {exc_name}: {exc}"},
        )

    app.include_router(health_router.router)
    app.include_router(traces_router.router)
    app.include_router(users_router.router)
    app.include_router(stats_router.router)
    app.include_router(search_router.router)
    app.include_router(errors_router.router)

    return app


app = create_app()
