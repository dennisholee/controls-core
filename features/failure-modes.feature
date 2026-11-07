@e2e
Feature: Failure behaviour is explicit
  Entitlement and masking fail closed. DQ blocks only when a rule is explicitly
  blocking. Error bodies are masked on endpoints that ask for it.

  Background:
    Given the controls bundle "integration/testdata/config"
    And the PIP serves the fixture attributes

  Scenario: An unregistered route is denied
    Given subject "u-1"
    When they GET "/admin/secrets"
    Then the request is denied
    And the status is 403

  @engine
  Scenario: A PIP outage fails closed
    Given the PIP is unavailable
    And subject "u-1"
    When they GET "/accounts/A123"
    Then the request is denied
    And the status is 503

  @engine
  Scenario: Upstream error bodies are masked on error-masking endpoints
    Given subject "u-1"
    And the upstream status is 500
    And the upstream responds with:
      """
      {"error":"boom","ssn":"111-22-3333"}
      """
    When they GET "/accounts/A123"
    Then the request is allowed
    And the status is 500
    And the response body is masked as:
      | path | value      |
      | ssn  | [REDACTED] |
