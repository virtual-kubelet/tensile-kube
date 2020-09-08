#
# Copyright ©2020. The virtual-kubelet authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#


REGISTRY_NAME=xxxx
GIT_COMMIT=$(shell git rev-parse "HEAD^{commit}")
VERSION=$(shell git describe --tags --abbrev=14 "${GIT_COMMIT}^{commit}" --always)
BUILD_TIME=$(shell TZ=Asia/Shanghai date +%FT%T%z)

CMDS=build-vk
all: test build

build: fmt vet provider scheduler webhook descheduler

fmt:
	go fmt ./pkg/...

vet:
	go vet ./pkg/...

provider:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux go build -ldflags "-X 'main.buildVersion=$(VERSION)' -X 'main.buildTime=${BUILD_TIME}'" -o ./bin/virtual-node ./cmd/provider

scheduler:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux go build -ldflags "-X 'main.buildVersion=$(VERSION)' -X 'main.buildTime=${BUILD_TIME}'" -o ./bin/multi-scheduler ./cmd/scheduler

webhook:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux go build -ldflags "-X 'github.com/virtual-kubelet/tensile-kube/cmd/webhook/app.Version=$(VERSION)'" -o ./bin/webhook ./cmd/webhook

descheduler:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux go build -ldflags "-X 'github.com/virtual-kubelet/tensile-kube/cmd/descheduler/app.version=$(VERSION)'" -o ./bin/descheduler ./cmd/descheduler

container: container-provider container-webhook container-descheduler

container-provider: provider
	docker build -t $(REGISTRY_NAME)/virtual-node:$(VERSION) -f $(shell if [ -e ./cmd/$*/Dockerfile ]; then echo ./cmd/$*/Dockerfile; else echo Dockerfile.provider; fi) --label revision=$(REV) .

container-webhook: webhook
	docker build -t $(REGISTRY_NAME)/virtual-webhook:$(VERSION) -f $(shell if [ -e ./cmd/$*/Dockerfile ]; then echo ./cmd/$*/Dockerfile; else echo Dockerfile.webhook; fi) --label revision=$(REV) .

container-descheduler: descheduler
	docker build -t $(REGISTRY_NAME)/descheduler:$(VERSION) -f $(shell if [ -e ./cmd/$*/Dockerfile ]; then echo ./cmd/$*/Dockerfile; else echo Dockerfile.descheduler; fi) --label revision=$(REV) .

push: container
	docker push $(REGISTRY_NAME)/virtual-k8s:$(VERSION)

test:
	go test -count=1 ./pkg/...