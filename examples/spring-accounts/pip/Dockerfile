FROM alpine:3.20
ARG PIP_BIN=bin/pip
RUN adduser -D -u 10001 controls
COPY ${PIP_BIN} /usr/local/bin/pip
COPY examples/spring-accounts/pip/attributes.json /etc/controls/attributes.json
USER controls
ENTRYPOINT ["/usr/local/bin/pip"]
