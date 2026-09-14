# 剧库：Go 静态编译 + Debian FFmpeg（含 libx264 / AAC）
# 本地：docker compose up --build
# Render：监听 $PORT，数据写到 /data 持久盘，必须设置 Basic Auth。
FROM golang:1.24-bookworm AS build
WORKDIR /src
COPY go.mod main.go ./
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false -ldflags="-s -w" -o /out/juku .

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ffmpeg ca-certificates tzdata tini \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --uid 1000 --shell /usr/sbin/nologin juku \
    && mkdir -p /data /downloads \
    && chown -R juku:juku /data /downloads
COPY --from=build /out/juku /usr/local/bin/juku
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod 755 /usr/local/bin/docker-entrypoint.sh
USER juku
WORKDIR /data
ENV TZ=Asia/Shanghai \
    JUKU_FFMPEG=ffmpeg
EXPOSE 8999
VOLUME ["/data", "/downloads"]
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/docker-entrypoint.sh"]
