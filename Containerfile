FROM registry.access.redhat.com/ubi8/go-toolset:latest AS builder
COPY . .
RUN go build ./cmd/ci-flakiness

FROM registry.access.redhat.com/ubi8/ubi-minimal:latest
# actions/checkout@v2 needs git-core and tar
RUN microdnf install -y git-core tar && microdnf clean all
COPY --from=builder /opt/app-root/src/ci-flakiness /ci-flakiness
