# Supported runtime versions

Status: current as of 2026-11-08. This is the policy used by `make eol-check`;
update it with every dependency or base-image change.

Removal schedule: [`docs/DEPRECATION.md`](DEPRECATION.md).

| Component | Supported | Notes |
|---|---|---|
| Java | 21, 25 | Java 21 is the demo target; 25 is the current LTS-friendly runtime |
| Spring Boot (service demos) | 4.1.x | Spring Boot 3.5 OSS support ended 2026-06-30 |
| OpenAPI Generator | 7.27.x | Generation timestamp hidden for reproducible builds |
| Go | 1.26.x, 1.27.x | `go.mod` currently 1.26.0 |
| Envoy | 1.40.x | Required for `allow_content_length_header`; use `HttpProtocolOptions` |
| Alpine (runtime images) | 3.23, 3.24 | 3.20 is EOL; images are digest-pinned |
| PIP API contract | 1.0.x | Sidecar verifies the handshake at startup |
| Cedar / OPA | optional | OPA policies are outside the signed bundle; benchmark before high RPS |

## EOL rules

- A base image or framework past OSS support fails `make eol-check` unless an
  explicit exception is recorded here with an owner and removal date.
- Runtime images use a tag **and** a digest. Do not use a floating tag.
- Upgrade order: contracts → PIP → sidecar → service demo. Run the pilot
  checklist after cluster-affecting changes.

## References

- Alpine releases: <https://alpinelinux.org/releases/>
- Spring Boot support: <https://endoflife.date/spring-boot>
- OpenAPI Generator: <https://repo.maven.apache.org/maven2/org/openapitools/openapi-generator-maven-plugin/maven-metadata.xml>
