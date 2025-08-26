FROM islandora/leptonica:main@sha256:a028717811d99ad90d3e2b222d4dd5e41c3a014006453b2137f41253798e0d56 AS leptonica
FROM ghcr.io/lehigh-university-libraries/scyllaridae-imagemagick:main AS build

WORKDIR /app

RUN --mount=type=bind,from=leptonica,source=/packages,target=/packages \
    --mount=type=bind,from=leptonica,source=/etc/apk/keys,target=/etc/apk/keys \
    apk update && \
    apk add --no-cache \
      build-base \
      go \
      /packages/leptonica-*.apk \
      tesseract-ocr-dev \
      fontconfig \
      ttf-dejavu \
      poppler-utils

COPY --chown=nobody:nogroup main.go go.* docker-entrypoint.sh ./
COPY --chown=nobody:nogroup internal/ ./internal/

ENV CGO_ENABLED=1

RUN go mod download && \
    go build -o /app/hocr && \
    go clean -cache -modcache

FROM ghcr.io/lehigh-university-libraries/scyllaridae-imagemagick:main

WORKDIR /app

RUN --mount=type=bind,from=leptonica,source=/packages,target=/packages \
    --mount=type=bind,from=leptonica,source=/etc/apk/keys,target=/etc/apk/keys \
    apk update && \
    apk add --no-cache \
      /packages/leptonica-*.apk \
      tesseract-ocr \
      tesseract-ocr-data-eng \
      fontconfig \
      ttf-dejavu \
      poppler-utils && \
    adduser -S -G nobody -u 8888 hocr

COPY --from=build /app/hocr /app/hocr
COPY --chown=hocr:hocr static/ ./static/
COPY --chown=hocr:hocr docker-entrypoint.sh ./

RUN mkdir uploads cache && \
    chown -R hocr uploads cache

ENTRYPOINT ["/bin/bash"]
CMD ["/app/docker-entrypoint.sh"]

HEALTHCHECK CMD curl -s http://localhost:8888/healthcheck
