# Archived policy from payments-service-demo, deleted 2026-11-08.
# Retained as an external-OPA example. It is not wired to a running sample.
# The demo repository history is in /tmp/payments-service-demo-final.bundle.

package controls.authz

import future.keywords.if

default allow := false

# Account reads: owner, same region, or support.
allow if {
  input.action == "read"
  input.subject.sub == input.resource.ownerId
}

allow if {
  input.action == "read"
  input.subject.claims.region == input.resource.region
}

allow if {
  input.subject.claims.role == "support"
}

# Payment writes: owner or support.
allow if {
  input.action == "write"
  input.subject.sub == input.resource.ownerId
}
