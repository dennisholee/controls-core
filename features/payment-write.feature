@e2e
Feature: Authorized writes and request data quality
  A write is authorized against the resource named in the registry, the payload
  self-description is only a cross-check, and blocking DQ rules stop the request
  before the upstream sees it.

  Background:
    Given the controls bundle "integration/testdata/config"
    And the PIP serves the fixture attributes
    And the upstream responds with:
      """
      {"id":"P-9001","status":"accepted"}
      """

  Scenario: An owner creates a payment
    Given subject "u-1"
    And the upstream status is 201
    When they POST "/payments" with JSON:
      """
      {"_type":"payment","_id":"P-9001","amount":10,"currency":"USD","ownerId":"u-1"}
      """
    Then the request is allowed
    And the response body is masked as:
      | path   | value    |
      | id     | P-9001   |
      | status | accepted |

  Scenario: A self-described account on the payment route is rejected
    Given subject "u-1"
    When they POST "/payments" with JSON:
      """
      {"_type":"account","_id":"P-9001","amount":10,"ownerId":"u-1"}
      """
    Then the request is denied
    And the status is 403

  @engine
  Scenario: The self-description mismatch is reported as the decision reason
    Given subject "u-1"
    When they POST "/payments" with JSON:
      """
      {"_type":"account","_id":"P-9001","amount":10,"ownerId":"u-1"}
      """
    Then the request is denied
    And the decision reason is "self_describing_type_mismatch"

  Scenario: A payment carrying a PAN is rejected before the upstream
    Given subject "u-1"
    When they POST "/payments" with JSON:
      """
      {"_type":"payment","_id":"P-9001","amount":10,"ownerId":"u-1","pan":"4111111111111111"}
      """
    Then the request is denied
    And the status is 403

  @engine
  Scenario: The rejected field is named in the decision reason
    Given subject "u-1"
    When they POST "/payments" with JSON:
      """
      {"_type":"payment","_id":"P-9001","amount":10,"ownerId":"u-1","pan":"4111111111111111"}
      """
    Then the request is denied
    And the decision reason is "rejected_field:pan"

  Scenario: A payment with a non-positive amount is blocked by DQ
    Given subject "u-1"
    When they POST "/payments" with JSON:
      """
      {"_type":"payment","_id":"P-9001","amount":-5,"ownerId":"u-1"}
      """
    Then the request is denied
    And the status is 403

  @engine
  Scenario: The blocking DQ rule is named in the decision reason
    Given subject "u-1"
    When they POST "/payments" with JSON:
      """
      {"_type":"payment","_id":"P-9001","amount":-5,"ownerId":"u-1"}
      """
    Then the request is denied
    And the decision reason is "dq_blocking:payment.amount.positive"
