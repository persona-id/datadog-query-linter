FROM alpine:3.18

LABEL org.opencontainers.image.description "Datadog query linter, for use mainly in CI"

COPY --chmod=755 datadog-query-linter /app/

WORKDIR /app

ENTRYPOINT ["/app/datadog-query-linter"]