# Datadog Query Linter

TL;DR: Simple process that will accept a list of yaml files, parse them to extract a datadog query, and validate said query.

## Rationale

In response to a recent incident, we want to have a CI process that we can drop into our kubernetes CI/CD pipelines that lints datadog queries. The specific queries in question here are the ones that control the HPAs across the infra, but it could technically [probably?] be any datadog query.

This is a tiny Go process, which will be put into a docker image for use in our Buildkite CI pipelines. The process accepts a list of files as the argument(s):

```bash
./datadog-query-linter \
    ../kubernetes/rendered/staging-europe/web/datadogmetric-web-worker.yaml \
    ../kubernetes/rendered/prod-us-central/web/datadogmetric-cold-storage-latency.yaml \
    ../kubernetes/rendered/staging-us-central1/web/datadogmetric-retention-workflow-latency.yaml
```

You can also use `find` to get said files:

```
./datadog-query-linter `find ../kubernetes/rendered -type f -name "datadogmetric-*"`
```

## Development

Clone the repo and it should just be ready to go. The Makefile has some assumptions about location of code (like it assume the k8s repo is in the parent directory), but otherwise it should work fine.

### Running

```bash
go mod tidy

source ./.envrc

make # or make test, make run, etc
```
