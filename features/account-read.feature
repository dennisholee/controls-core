@e2e
Feature: Data-level entitlement for account reads
  The registry binds the route to a resource type; the PIP supplies the
  resource's owner and region. The decision is made before the upstream body is
  returned, and masking is applied to that body on the way out.

  Background:
    Given the controls bundle "integration/testdata/config"
    And the PIP serves the fixture attributes
    And the upstream responds with:
      """
      {"accountNumber":"1234567890","holder":{"email":"jane.doe@example.com"},"ssn":"111-22-3333","region":"US","status":"active"}
      """

  Scenario: The owner reads their own account and sensitive fields are masked
    Given subject "u-1"
    When they GET "/accounts/A123"
    Then the request is allowed
    And the status is 200
    And the response body is masked as:
      | path          | value           |
      | accountNumber | ******7890      |
      | ssn           | [REDACTED]      |
      | holder.email  | j***@example.com |
      | region        | US              |

  Scenario: A subject in the account's region is allowed without owning it
    Given subject "u-9" with claim "region" = "US"
    When they GET "/accounts/A123"
    Then the request is allowed
    And the status is 200

  Scenario: A subject outside the account's region is denied
    Given subject "u-2" with claim "region" = "EU"
    When they GET "/accounts/A123"
    Then the request is denied
    And the status is 403

  Scenario: A sanctioned owner is denied even when they own the account
    Given subject "u-1" with claim "status" = "sanctioned"
    When they GET "/accounts/A123"
    Then the request is denied
    And the status is 403
