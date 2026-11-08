FROM alpine:3.24@sha256:3f4d6b32e26703c6082839234f43769d5046b3ac7c910033911d1e71d8cd630a
ARG PIP_BIN=bin/pip
RUN adduser -D -u 10001 controls
COPY ${PIP_BIN} /usr/local/bin/pip
COPY examples/payments-write/pip/attributes.json /etc/controls/attributes.json
USER controls
ENTRYPOINT ["/usr/local/bin/pip"]
