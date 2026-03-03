FROM mcr.microsoft.com/playwright:v1.47.0-noble AS pwbase


FROM mcr.microsoft.com/oss/go/microsoft/golang:1.21 AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# 安装 playwright-go 所需 driver（浏览器直接复用 Playwright 官方镜像中的 /ms-playwright）
COPY --from=pwbase /ms-playwright /ms-playwright
ENV PLAYWRIGHT_BROWSERS_PATH=/ms-playwright
RUN go run github.com/playwright-community/playwright-go/cmd/playwright@v0.4700.0 install chromium

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o /out/bilibili-mcp ./cmd/server


FROM pwbase

WORKDIR /app

COPY --from=builder /out/bilibili-mcp /app/bilibili-mcp
COPY config.docker.yaml /app/config.yaml

# playwright-go driver 默认安装在 root cache 目录下
COPY --from=builder /root/.cache/ms-playwright-go /root/.cache/ms-playwright-go

RUN mkdir -p /app/cookies /app/logs /app/models /app/downloads

ENV PLAYWRIGHT_BROWSERS_PATH=/ms-playwright
USER root

EXPOSE 18666

ENTRYPOINT ["/app/bilibili-mcp", "-config", "/app/config.yaml"]
