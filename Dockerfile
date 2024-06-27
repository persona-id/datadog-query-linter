FROM alpine:3.18

COPY --chmod=700 datadog-query-linter /app/

WORKDIR /app

USER agent

ENTRYPOINT ["/app/datadog-query-linter"]