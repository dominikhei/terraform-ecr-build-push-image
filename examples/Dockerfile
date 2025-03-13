FROM registry.fedoraproject.org/fedora-minimal:40 as build

ARG VERSION=2.9.3

WORKDIR /src

RUN microdnf -y --nodocs install hostname make protobuf-devel golang git \
  golang-github-gogo-protobuf systemd-devel
RUN git clone --depth 1 --branch v${VERSION} https://github.com/grafana/loki.git /src
RUN make clean && make BUILD_IN_CONTAINER=false PROMTAIL_JOURNAL_ENABLED=true promtail

FROM registry.fedoraproject.org/fedora-minimal:40

RUN microdnf -y --nodocs install systemd-libs && microdnf clean all && rm -rf /var/lib/dnf /var/cache/*

ENV ASSUME_NO_MOVING_GC_UNSAFE_RISK_IT_WITH=go1.18
ENV TEST=1

COPY --from=build /src/clients/cmd/promtail/promtail /usr/bin/promtail
COPY --from=build /src/clients/cmd/promtail/promtail-docker-config.yaml /etc/promtail/config.yml

ENTRYPOINT ["/usr/bin/promtail"]

CMD ["-config.file=/etc/promtail/config.yml"]