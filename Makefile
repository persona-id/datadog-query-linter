SHELL := /bin/bash

# The name of the executable
TARGET := 'datadog-query-linter'

# Use linker flags to provide version/build settings to the target.
LDFLAGS=-ldflags "-s -w"

all: clean lint build

$(TARGET):
	@go build $(LDFLAGS) -o $(TARGET) main.go

build: clean $(TARGET)
	@true

clean:
	@rm -rf $(TARGET) *.test *.out tmp/* coverage dist

lint:
	@gofmt -s -l -w .
	@go vet ./...
	@golangci-lint run --config=.golangci.yml --allow-parallel-runners

test:
	@mkdir -p coverage
	@go test ./... --shuffle=on --coverprofile coverage/coverage.out

coverage: test
	@go tool cover -html=coverage/coverage.out

# run the process against the files in the tests/ directory
run: build
	./$(TARGET) `find ./tests -type f -name *.yaml`

# run the process against our full rendered directory
runfull: build
	./$(TARGET) `find ../kubernetes/rendered -type f -name "datadogmetric-*"`

snapshot: clean lint
	@goreleaser --snapshot --clean

release: clean lint
	@goreleaser --clean

dockertest:
	@echo $$(pwd)/tests
	@docker container run --rm \
		--user $$(id -u ${USER}):$$(id -g ${USER}) \
		--volume $$(pwd)/tests:/tests \
		--env DD_CLIENT_API_KEY=${DD_API_KEY} \
		--env DD_CLIENT_APP_KEY=${DD_APP_KEY} \
		ghcr.io/persona-id/datadog-query-linter:latest \
		/usr/local/bin/datadog-query-linter \
		$$(ls tests/*.yaml)
