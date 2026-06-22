export GOEXPERIMENT=jsonv2

MAIN_PG_PATH := main/pg
MAIN_MYSQL_PATH := main/mysql
MAIN_SQLSERVER_PATH := main/sqlserver
MAIN_REDIS_PATH := main/redis
MAIN_MONGO_PATH := main/mongo
MAIN_FDB_PATH := main/fdb
MAIN_GP_PATH := main/gp
MAIN_ETCD_PATH := main/etcd
DOCKER_COMMON := golang ubuntu ubuntu_22_04 s3
CMD_FILES = $(wildcard cmd/**/*.go)
PKG_FILES = $(wildcard internal/*.go internal/**/*.go internal/**/**/*.go internal/**/**/**/*.go)
TEST_FILES = $(wildcard test/*.go testtools/*.go)
PKG := github.com/lateos-ai/wal-g
BUILD_DATE := $(shell date -u +%Y.%m.%d_%H:%M:%S 2>/dev/null || powershell -Command "Get-Date -Format 'yyyy.MM.dd_HH:mm:ss'" 2>/dev/null || echo unknown)
COVERAGE_FILE := coverage.out
TEST := "pg10_tests"
MYSQL_TEST := "mysql_base_tests"
MYSQL8_TEST := "mysql8_tests"
MONGO_VERSION ?= "8.0.3"
MONGO_PACKAGE ?= "mongodb-org"
MONGO_REPO ?= "repo.mongodb.org"
MONGO_TEST_TYPE ?= "all"
GOLANGCI_LINT_VERSION ?= "v2.0"
REDIS_VERSION ?= "6.2.4"
S3_IMAGE := minio/minio:RELEASE.2021-06-07T21-40-51Z
S3_THROTTLING_IMAGE := minio/minio:RELEASE.2024-01-18T22-51-28Z
SWIFT_IMAGE := openstackswift/saio:py3
PULL_RETRIES := 3
IMAGE_TYPE ?= "rdb"
MOCKS_DESTINATION := ./testtools/mocks
FILE_TO_MOCKS := ./internal/uploader.go # list interface paths here
WALG_VERSION ?= `git tag -l --points-at HEAD | tail -1`
GIT_REVISION ?= `git rev-parse --short HEAD`

BUILD_TAGS:=

ifdef USE_BROTLI
	BUILD_TAGS += brotli
endif

ifdef USE_LIBSODIUM
	BUILD_TAGS += libsodium
endif

ifdef USE_LZO
	BUILD_TAGS += lzo
endif

BUILD_TAGS := $(strip $(BUILD_TAGS))

# Tags for vet/test – exclude libsodium to avoid transitive cgo build
# failure (the libsodium package is verified separately non-fatally).
LIBTAGS := $(filter-out libsodium,$(BUILD_TAGS))

ifdef USE_LIBSODIUM
	# Provide CGo flags via environment variables and pkg-config.
	# The source files use #cgo pkg-config: libsodium which calls
	# pkg-config --cflags --libs libsodium. We make this resolve to
	# the tmp/libsodium/ tree by exporting PKG_CONFIG_PATH and by
	# having link_libsodium.sh generate a libsodium.pc there.
	# CGO_CFLAGS/CGO_LDFLAGS are kept as fallback for systems that
	# lack pkg-config (they are concatenated with pkg-config results).
	LIBSODIUM_CFLAGS := -I$(CURDIR)/tmp/libsodium/include -I$(CURDIR)/tmp/libsodium/include/sodium
	LIBSODIUM_LDFLAGS := -L$(CURDIR)/tmp/libsodium/lib -lsodium
	export CGO_ENABLED=1
	export CGO_CFLAGS += $(LIBSODIUM_CFLAGS)
	export CGO_LDFLAGS += $(LIBSODIUM_LDFLAGS)
	export PKG_CONFIG_PATH := $(CURDIR)/tmp/libsodium/lib/pkgconfig$(if $(PKG_CONFIG_PATH),:$(PKG_CONFIG_PATH))
	# Capture for explicit prefixing right before the 'go' command.
	# Recipes use the form "cd $(DIR) && $(SODIUM_CGO) go build ..." so that
	# the env var assignments are valid shell syntax (avoids "VAR=val (cd ...)"
	# which /bin/sh rejects).
	SODIUM_CGO := CGO_ENABLED=1 CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" PKG_CONFIG_PATH="$(PKG_CONFIG_PATH)"
endif

BUILD_GCFLAGS := 

ifdef ENABLE_DEBUG
	BUILD_GCFLAGS:=$(BUILD_GCFLAGS) all=-N -l
endif

# Retry a command up to N times with exponential backoff.
# Usage: $(call retry,3,command arg1 arg2)
retry = for i in $$(seq 1 $(1)); do \
  echo "Attempt $$i/$(1): $(2)"; \
  if $(2); then \
    echo "Success on attempt $$i"; \
    break; \
  elif [ $$i -eq $(1) ]; then \
    echo "Failed after $(1) attempts"; \
    exit 1; \
  else \
    sleep $$((1 << ($$i - 1))); \
  fi; \
done

# Pull external Docker images with retry (e.g., s3 from Docker Hub).
# External images are not built locally and may fail on transient network errors.
pull_external_images:
	@if [ -f ${CACHE_FILE_S3} ]; then docker load -i ${CACHE_FILE_S3} && echo "Loaded S3 from cache"; fi
	@if [ -f ${CACHE_FILE_S3_THROTTLING} ]; then docker load -i ${CACHE_FILE_S3_THROTTLING} && echo "Loaded S3 throttling from cache"; fi
	for i in $$(seq 1 $(PULL_RETRIES)); do \
		if docker compose pull s3 s3-another s3-for-throttling 2>/dev/null; then \
			echo "Successfully pulled external images"; \
			break; \
		elif [ $$i -eq $(PULL_RETRIES) ]; then \
			echo "Warning: failed to pull external images after $(PULL_RETRIES) attempts, continuing"; \
		else \
			sleep $$((1 << ($$i - 1))); \
		fi; \
	done

.PHONY: unittest fmt lint clean

test: deps unittest pg_build mysql_build redis_build mongo_build gp_build cloudberry_build unlink_brotli pg_integration_test mysql_integration_test redis_integration_test fdb_integration_test gp_integration_test cloudberry_integration_test etcd_integration_test

pg_test: deps pg_build unlink_brotli pg_integration_test

pg_build: $(CMD_FILES) $(PKG_FILES)
	cd $(MAIN_PG_PATH) && $(SODIUM_CGO) go build -mod vendor -tags "$(BUILD_TAGS)" -o wal-g -gcflags "$(BUILD_GCFLAGS)" -ldflags "-s -w -X $(PKG)/cmd/pg.buildDate=$(BUILD_DATE) -X $(PKG)/cmd/pg.gitRevision=$(GIT_REVISION) -X $(PKG)/cmd/pg.walgVersion=$(WALG_VERSION)"

install_and_build_pg: deps pg_build

pg10_build_image: pull_external_images
	# There are dependencies between container images.
	# Running in one command leads to using outdated images and fails on clean system.
	# It can not be fixed with depends_on in compose file. https://github.com/docker/compose/issues/6332
	docker compose build $(DOCKER_COMMON)
	docker compose build pg10
	docker compose build pg10_tests_template

pg18_build_image: pull_external_images
	docker compose build $(DOCKER_COMMON)
	docker compose build pg18
	docker compose build pg18_tests_template

pg_save_image: install_and_build_pg pg10_build_image pg18_build_image
	mkdir -p ${CACHE_FOLDER}
	sudo rm -rf ${CACHE_FOLDER}/*
	docker save ${IMAGE_PG10_TESTS} > ${CACHE_FILE_PG10_TESTS}
	docker save ${IMAGE_PG18_TESTS} > ${CACHE_FILE_PG18_TESTS}
	docker save wal-g/ubuntu:18.04 > ${CACHE_FILE_UBUNTU_18_04}
	docker save wal-g/ubuntu:22.04 > ${CACHE_FILE_UBUNTU_22_04}
	docker save ${IMAGE_GOLANG}    > ${CACHE_FILE_GOLANG}
	for i in $$(seq 1 $(PULL_RETRIES)); do \
		if docker pull ${S3_IMAGE} 2>/dev/null; then \
			docker save ${S3_IMAGE} > ${CACHE_FILE_S3} && echo "Cached ${S3_IMAGE}"; \
			break; \
		elif [ $$i -eq $(PULL_RETRIES) ]; then \
			echo "Warning: failed to pull ${S3_IMAGE}, skipping cache"; \
		else \
			sleep $$((1 << ($$i - 1))); \
		fi; \
	done
	for i in $$(seq 1 $(PULL_RETRIES)); do \
		if docker pull ${S3_THROTTLING_IMAGE} 2>/dev/null; then \
			docker save ${S3_THROTTLING_IMAGE} > ${CACHE_FILE_S3_THROTTLING} && echo "Cached ${S3_THROTTLING_IMAGE}"; \
			break; \
		elif [ $$i -eq $(PULL_RETRIES) ]; then \
			echo "Warning: failed to pull ${S3_THROTTLING_IMAGE}, skipping cache"; \
		else \
			sleep $$((1 << ($$i - 1))); \
		fi; \
	done
	for i in $$(seq 1 $(PULL_RETRIES)); do \
		if docker pull ${SWIFT_IMAGE} 2>/dev/null; then \
			docker save ${SWIFT_IMAGE} > ${CACHE_FILE_SWIFT} && echo "Cached ${SWIFT_IMAGE}"; \
			break; \
		elif [ $$i -eq $(PULL_RETRIES) ]; then \
			echo "Warning: failed to pull ${SWIFT_IMAGE}, skipping cache"; \
		else \
			sleep $$((1 << ($$i - 1))); \
		fi; \
	done
	ls ${CACHE_FOLDER}

pg_integration_test: clean_compose pull_external_images
	@if [ "x" = "${CACHE_FILE_PG10_TESTS}x" ]; then\
		echo "Rebuild";\
		make install_and_build_pg;\
		make pg10_build_image;\
	else\
		docker load -i ${CACHE_FILE_PG10_TESTS} && rm ${CACHE_FILE_PG10_TESTS};\
	fi
	@if echo "$(TEST)" | grep -Fqe "pg18"; then\
		if [ -f ${CACHE_FILE_PG18_TESTS} ]; then\
			docker load -i ${CACHE_FILE_PG18_TESTS} && rm ${CACHE_FILE_PG18_TESTS};\
		else\
			make pg18_build_image;\
		fi;\
	fi
	@if echo "$(TEST)" | grep -Fqe "pgbackrest"; then\
		docker compose build pg10_pgbackrest;\
	fi
	@if echo "$(TEST)" | grep -Fq -e "pg10_ssh_" -e "pg10_storage_ssh_"; then\
		docker compose build ssh;\
	fi
	@if echo "$(TEST)" | grep -Fqe "swift"; then\
		if [ -f ${CACHE_FILE_SWIFT} ]; then\
			docker load -i ${CACHE_FILE_SWIFT} && rm ${CACHE_FILE_SWIFT};\
		else\
			docker compose build swift;\
		fi;\
	fi

	docker compose up --pull never --exit-code-from $(TEST) $(TEST)
	# Run tests with dependencies if we run all tests
	@if [ "$(TEST)" = "pg10_tests" ]; then\
		docker compose build pg10_pgbackrest ssh swift pg10_wal_perftest_with_throttling &&\
		docker compose up --pull never --exit-code-from pg10_ssh_backup_test pg10_ssh_backup_test &&\
		docker compose up --pull never --exit-code-from pg10_storage_swift_test pg10_storage_swift_test &&\
		docker compose up --pull never --exit-code-from pg10_storage_ssh_test pg10_storage_ssh_test &&\
		docker compose up --pull never --exit-code-from pg10_pgbackrest_backup_fetch_test pg10_pgbackrest_backup_fetch_test &&\
		docker compose down &&\
		docker compose up --pull never --exit-code-from pg10_wal_perftest_with_throttling pg10_wal_perftest_with_throttling ;\
	fi
	make clean_compose

orioledb_integration_test: install_and_build_pg clean_compose pull_external_images load_docker_common
	docker compose build orioledb
	docker compose up --pull never --exit-code-from orioledb orioledb
	make clean_compose

.PHONY: clean_compose
clean_compose:
	services=$$(docker compose ps -a --format '{{.Name}} {{.Service}}' | grep wal-g_ | cut -d' ' -f 2); \
		if [ "$$services" ]; then docker compose down $$services; fi

all_unittests: deps unittest

# todo Should we remove this target as a duplicate of pg_integration_test?
pg_int_tests_only: pull_external_images
	docker compose build pg10_tests
	docker compose up --pull never --exit-code-from pg10_tests pg10_tests

pg_clean:
	(cd $(MAIN_PG_PATH) && go clean)
	./cleanup.sh

pg_install: pg_build
	mv $(MAIN_PG_PATH)/wal-g $(GOBIN)/wal-g

mysql_base: deps mysql_build unlink_brotli
mysql_test: deps mysql_build unlink_brotli mysql_integration_test

mysql_build: $(CMD_FILES) $(PKG_FILES)
	cd $(MAIN_MYSQL_PATH) && $(SODIUM_CGO) go build -mod vendor -tags "$(BUILD_TAGS)" -o wal-g -gcflags "$(BUILD_GCFLAGS)" -ldflags "-s -w -X $(PKG)/cmd/mysql.buildDate=$(BUILD_DATE) -X $(PKG)/cmd/mysql.gitRevision=$(GIT_REVISION) -X $(PKG)/cmd/mysql.walgVersion=$(WALG_VERSION)"

sqlserver_build: $(CMD_FILES) $(PKG_FILES)
	cd $(MAIN_SQLSERVER_PATH) && $(SODIUM_CGO) go build -mod vendor -tags "$(BUILD_TAGS)" -o wal-g -gcflags "$(BUILD_GCFLAGS)" -ldflags "-s -w -X $(PKG)/cmd/sqlserver.buildDate=$(BUILD_DATE) -X $(PKG)/cmd/sqlserver.gitRevision=$(GIT_REVISION) -X $(PKG)/cmd/sqlserver.walgVersion=$(WALG_VERSION)"

load_docker_common:
	@if [ "x" = "${CACHE_FOLDER}x" ]; then\
		echo "Rebuild";\
		docker compose build $(DOCKER_COMMON);\
		for i in $$(seq 1 $(PULL_RETRIES)); do \
			if docker compose pull s3 s3-another s3-for-throttling 2>/dev/null; then \
				echo "Successfully pulled external images"; \
				break; \
			elif [ $$i -eq $(PULL_RETRIES) ]; then \
				echo "Warning: failed to pull external images after $(PULL_RETRIES) attempts, continuing"; \
			else \
				sleep $$((1 << ($$i - 1))); \
			fi; \
		done;\
	else\
		docker load -i ${CACHE_FILE_UBUNTU_18_04} && rm ${CACHE_FILE_UBUNTU_18_04};\
		docker load -i ${CACHE_FILE_UBUNTU_22_04} && rm ${CACHE_FILE_UBUNTU_22_04};\
		docker load -i ${CACHE_FILE_GOLANG} && rm ${CACHE_FILE_GOLANG};\
		if [ -f ${CACHE_FILE_S3} ]; then docker load -i ${CACHE_FILE_S3} && rm ${CACHE_FILE_S3}; fi;\
		if [ -f ${CACHE_FILE_S3_THROTTLING} ]; then docker load -i ${CACHE_FILE_S3_THROTTLING} && rm ${CACHE_FILE_S3_THROTTLING}; fi;\
	fi

mysql_integration_test: deps mysql_build unlink_brotli pull_external_images load_docker_common
	./link_brotli.sh
	docker compose build mysql && docker compose build $(MYSQL_TEST)
	docker compose up --pull never --force-recreate --exit-code-from $(MYSQL_TEST) $(MYSQL_TEST)

mysql8_integration_test: go_deps unlink_brotli pull_external_images load_docker_common
	docker compose build mysql8 && docker compose build $(MYSQL8_TEST)
	docker compose up --pull never --force-recreate --exit-code-from $(MYSQL8_TEST) $(MYSQL8_TEST)

mysql_clean:
	(cd $(MAIN_MYSQL_PATH) && go clean)
	./cleanup.sh

mysql_install: mysql_build
	mv $(MAIN_MYSQL_PATH)/wal-g $(GOBIN)/wal-g

mariadb_test: deps mysql_build unlink_brotli mariadb_integration_test

mariadb_integration_test: unlink_brotli pull_external_images load_docker_common
	./link_brotli.sh
	docker compose build mariadb && docker compose build mariadb_tests
	docker compose up --pull never --force-recreate --exit-code-from mariadb_tests mariadb_tests

mongo_test: deps mongo_build unlink_brotli

mongo_build: $(CMD_FILES) $(PKG_FILES)
	cd $(MAIN_MONGO_PATH) && $(SODIUM_CGO) go build -mod vendor -tags "$(BUILD_TAGS)" -o wal-g -gcflags "$(BUILD_GCFLAGS)" -ldflags "-s -w -X $(PKG)/cmd/mongo.buildDate=$(BUILD_DATE) -X $(PKG)/cmd/mongo.gitRevision=$(GIT_REVISION) -X $(PKG)/cmd/mongo.walgVersion=$(WALG_VERSION)"

mongo_install: mongo_build
	mv $(MAIN_MONGO_PATH)/wal-g $(GOBIN)/wal-g

mongo_features:
	set -e
	make go_deps
	cd tests_func/ && MONGO_VERSION=$(MONGO_VERSION) MONGO_PACKAGE=$(MONGO_PACKAGE) MONGO_REPO=$(MONGO_REPO) MONGO_TEST_TYPE=$(MONGO_TEST_TYPE) go test -v -count=1 -timeout 45m  --tf.test=true --tf.debug=true --tf.clean=false --tf.stop=false --tf.database=mongodb

mongo_binary_features:
	MONGO_TEST_TYPE="binary" $(MAKE) mongo_features

mongo_logical_features:
	MONGO_TEST_TYPE="logical" $(MAKE) mongo_features

mongo_partial_features:
	MONGO_TEST_TYPE="partial" $(MAKE) mongo_features

mongo_catch_up_features:
	MONGO_TEST_TYPE="catch_up" $(MAKE) mongo_features

clean_mongo_features:
	set -e
	cd tests_func/ && MONGO_VERSION=$(MONGO_VERSION) MONGO_PACKAGE=$(MONGO_PACKAGE) MONGO_REPO=$(MONGO_REPO) go test -v -count=1  -timeout 5m --tf.test=false --tf.debug=false --tf.clean=true --tf.stop=true --tf.database=mongodb

fdb_build: $(CMD_FILES) $(PKG_FILES)
	cd $(MAIN_FDB_PATH) && $(SODIUM_CGO) go build -mod vendor -tags "$(BUILD_TAGS)" -o wal-g -gcflags "$(BUILD_GCFLAGS)" -ldflags "-s -w"

fdb_install: fdb_build
	mv $(MAIN_FDB_PATH)/wal-g $(GOBIN)/wal-g

fdb_integration_test: pull_external_images load_docker_common
	docker compose down -v
	docker compose build fdb_tests
	docker compose up --pull never --force-recreate --renew-anon-volumes --exit-code-from fdb_tests fdb_tests

redis_test: deps redis_build unlink_brotli redis_integration_test

redis_build: $(CMD_FILES) $(PKG_FILES)
	cd $(MAIN_REDIS_PATH) && $(SODIUM_CGO) go build -mod vendor -tags "$(BUILD_TAGS)" -o wal-g -gcflags "$(BUILD_GCFLAGS)" -ldflags "-s -w -X $(PKG)/cmd/redis.buildDate=$(BUILD_DATE) -X $(PKG)/cmd/redis.gitRevision=$(GIT_REVISION) -X $(PKG)/cmd/redis.walgVersion=$(WALG_VERSION)"

redis_integration_test: pull_external_images load_docker_common
	docker compose build redis && docker compose build redis_tests
	docker compose up --pull never --exit-code-from redis_tests redis_tests

redis_clean:
	(cd $(MAIN_REDIS_PATH) && go clean)
	./cleanup.sh

redis_install: redis_build
	mv $(MAIN_REDIS_PATH)/wal-g $(GOBIN)/wal-g

redis_features:
	set -e
	make go_deps
	cd tests_func/ && REDIS_VERSION=$(REDIS_VERSION) IMAGE_TYPE=$(IMAGE_TYPE) go test -v -count=1 -timeout 20m  --tf.test=true --tf.debug=false --tf.clean=false --tf.stop=false --tf.database=redis

clean_redis_features:
	set -e
	cd tests_func/ && REDIS_VERSION=$(REDIS_VERSION) go test -v -count=1  -timeout 5m --tf.test=false --tf.debug=false --tf.clean=true --tf.stop=true --tf.database=redis

etcd_test: deps etcd_build unlink_brotli etcd_integration_test

etcd_build: $(CMD_FILES) $(PKG_FILES)
	cd $(MAIN_ETCD_PATH) && $(SODIUM_CGO) go build -x -mod vendor -tags "$(BUILD_TAGS)" -o wal-g -gcflags "$(BUILD_GCFLAGS)" -ldflags "-s -w -X $(PKG)/cmd/etcd.buildDate=$(BUILD_DATE) -X $(PKG)/cmd/etcd.gitRevision=$(GIT_REVISION) -X $(PKG)/cmd/etcd.walgVersion=$(WALG_VERSION)"

etcd_install: etcd_build
	mv $(MAIN_ETCD_PATH)/wal-g $(GOBIN)/wal-g

etcd_clean:
	(cd $(MAIN_ETCD_PATH) && go clean)
	./cleanup.sh

# refactor
etcd_integration_test: pull_external_images load_docker_common
	docker compose build etcd
	docker compose build etcd_tests
	docker compose up --pull never --exit-code-from etcd_tests etcd_tests

gp_build: $(CMD_FILES) $(PKG_FILES)
	cd $(MAIN_GP_PATH) && $(SODIUM_CGO) go build -mod vendor -tags "$(BUILD_TAGS)" -o wal-g -gcflags "$(BUILD_GCFLAGS)" -ldflags "-s -w -X $(PKG)/cmd/gp.buildDate=$(BUILD_DATE) -X $(PKG)/cmd/gp.gitRevision=$(GIT_REVISION) -X $(PKG)/cmd/gp.walgVersion=$(WALG_VERSION)"

gp_clean:
	(cd $(MAIN_GP_PATH) && go clean)
	./cleanup.sh

gp_install: gp_build
	mv $(MAIN_GP_PATH)/wal-g $(GOBIN)/wal-g

gp_test: deps gp_build unlink_brotli gp_integration_test

gp_integration_test: pull_external_images load_docker_common
	docker compose build gp
	docker compose build gp_tests
	docker compose up --pull never --exit-code-from gp_tests gp_tests

cloudberry_build:
	$(MAKE) gp_build USE_LIBSODIUM=

cloudberry_clean: gp_clean

cloudberry_install: gp_install

cloudberry_test: deps cloudberry_build unlink_brotli cloudberry_integration_test

cloudberry_integration_test: pull_external_images load_docker_common
	docker compose build cloudberry
	docker compose build cloudberry_tests
	docker compose up --pull never s3 cloudberry_tests --force-recreate --exit-code-from cloudberry_tests

st_test: deps pg_build unlink_brotli st_integration_test

st_integration_test: pull_external_images load_docker_common
	docker compose build st_tests
	docker compose up --pull never --exit-code-from st_tests st_tests

unittest:
	@echo "=== CGO DEBUG (libsodium) ==="
	@$(SODIUM_CGO) sh -c '\
	  echo "CGO_ENABLED=$$CGO_ENABLED"; \
	  echo "CGO_CFLAGS=$$CGO_CFLAGS"; \
	  echo "CGO_LDFLAGS=$$CGO_LDFLAGS"; \
	  go env CGO_ENABLED CGO_CFLAGS CGO_LDFLAGS; \
	  echo "sodium.h present at vet time?"; ls -l tmp/libsodium/include/sodium.h 2>/dev/null || echo "MISSING"; \
	  echo "grep for sodium_init in installed headers:"; \
	  grep -rn "sodium_init" tmp/libsodium/include/ 2>/dev/null | head -5 || echo "none found"; \
	  echo "head of sodium.h:"; head -30 tmp/libsodium/include/sodium.h 2>/dev/null || true; \
	  echo "=== direct C compile test with CGO flags ==="; \
	  printf "#include <sodium.h>\nint main(void){ return sodium_init(); }\n" > /tmp/test_sodium.c; \
	  gcc $$CGO_CFLAGS -c /tmp/test_sodium.c -o /tmp/test_sodium.o 2>&1 && echo "C compile with sodium.h: OK" || echo "C compile with sodium.h: FAILED"; \
	  echo "=== explicit cgo package build (forces full cgo processing) ==="; \
	  (cd internal/crypto/libsodium && go test -x -work -c -mod=vendor -tags "$(BUILD_TAGS)" -o /dev/null . ) 2>&1 | tail -20 || echo "(build test completed, see errors above if any)"; \
	  echo "=== END CGO DEBUG ===" ' || true
	$(SODIUM_CGO) go vet -mod=vendor -tags "$(LIBTAGS)" $(shell go list -mod=vendor -tags "$(LIBTAGS)" ./cmd/... ./internal/... ./pkg/... ./utility/... 2>/dev/null | grep -v '/libsodium' || true)
	$(SODIUM_CGO) go test -mod vendor -v $(TEST_MODIFIER) -tags "$(LIBTAGS)" $(shell go list -mod=vendor -tags "$(LIBTAGS)" ./internal/... 2>/dev/null | grep -v '/libsodium' || true )
	$(SODIUM_CGO) go test -mod vendor -v $(TEST_MODIFIER) -tags "$(LIBTAGS)" $(shell go list -mod=vendor -tags "$(LIBTAGS)" ./pkg/... 2>/dev/null | grep -v '/libsodium' || true )
	$(SODIUM_CGO) go test -mod vendor -v $(TEST_MODIFIER) -tags "$(LIBTAGS)" $(shell go list -mod=vendor -tags "$(LIBTAGS)" ./utility/... 2>/dev/null | grep -v '/libsodium' || true )

coverage:
	$(SODIUM_CGO) go test -mod vendor -v $(TEST_MODIFIER) -tags "$(LIBTAGS)" -coverprofile=$(COVERAGE_FILE) $(shell go list -mod=vendor -tags "$(LIBTAGS)" ./cmd/... ./internal/... ./pkg/... ./utility/... 2>/dev/null | grep -v '/libsodium' || true )
	go tool cover -html=$(COVERAGE_FILE)

fmt: $(CMD_FILES) $(PKG_FILES) $(TEST_FILES)
	go fmt ./...
	gofmt -s -w $(CMD_FILES) $(PKG_FILES) $(TEST_FILES)

lint:
	golangci-lint run --allow-parallel-runners ./...

docker_lint:
	docker build -t wal-g/lint --build-arg TAG=$(GOLANGCI_LINT_VERSION) - < docker/lint/Dockerfile
	docker run --rm -v `pwd`:/app \
		-v wal-g_lint_cache:/cache -e GOLANGCI_LINT_CACHE=/cache/lint \
		-e GOCACHE=/cache/go -e GOMODCACHE=/cache/gomod \
		wal-g/lint golangci-lint run -v

deps: go_deps link_external_deps

go_deps:
	git submodule update --init
	cp CMakeLists-brotli.txt submodules/brotli/CMakeLists.txt
	go mod vendor
ifdef USE_LZO
	if [ "$$(uname)" = "Darwin" ]; then \
		perl -pi -e 's|(#cgo LDFLAGS:) .*|$$1 -llzo2|' vendor/github.com/cyberdelia/lzo/lzo.go; \
	else \
		perl -pi -e 's|(#cgo LDFLAGS:) .*|$$1 -Wl,-Bstatic -llzo2 -Wl,-Bdynamic|' vendor/github.com/cyberdelia/lzo/lzo.go; \
	fi
endif

link_external_deps: link_brotli link_libsodium

unlink_external_deps: unlink_brotli unlink_libsodium

install:
	@echo "Nothing to be done. Use pg_install/mysql_install/mongo_install/fdb_install/gp_install/etcd_install... instead."

link_brotli:
	@if [ -n "${USE_BROTLI}" ]; then ./link_brotli.sh; fi
	@if [ -z "${USE_BROTLI}" ]; then echo "info: USE_BROTLI is not set, skipping 'link_brotli' task"; fi

link_libsodium:
	@if [ ! -z "${USE_LIBSODIUM}" ]; then\
		./link_libsodium.sh;\
		echo "info: libsodium tree after link_libsodium:"; ls -lR tmp/libsodium 2>/dev/null || true;\
		echo "info: sodium.h present?"; ls -l tmp/libsodium/include/sodium.h 2>/dev/null || echo "MISSING sodium.h";\
		echo "info: libsodium.a present?"; ls -l tmp/libsodium/lib/libsodium.a 2>/dev/null || echo "MISSING libsodium.a";\
		echo "info: include/ top level:"; ls tmp/libsodium/include/ 2>/dev/null | head -10 || true;\
	fi

unlink_brotli:
	rm -rf vendor/github.com/google/brotli/*
	if [ -n "${USE_BROTLI}" ] ; then mv tmp/brotli/* vendor/github.com/google/brotli/; fi
	rm -rf tmp/brotli

unlink_libsodium:
	rm -rf tmp/libsodium

build_client:
	cd cmd/daemonclient && \
	go build -o ../../bin/walg-daemon-client -gcflags "$(BUILD_GCFLAGS)" -ldflags "-s -w -X main.buildDate=$(BUILD_DATE) -X main.gitRevision=$(GIT_REVISION) -X main.version=$(WALG_VERSION)"

.PHONY: mocks
# put the files with interfaces you'd like to mock in prerequisites
# wildcards are allowed
mocks: $(FILE_TO_MOCKS)
	@echo "Generating mocks..."
	@rm -rf $(MOCKS_DESTINATION)
	@for file in $^; do mockgen -source=$$file -destination=$(MOCKS_DESTINATION)/$$(basename $$file); done
