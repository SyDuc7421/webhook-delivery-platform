# ADR-0005: Self-managed Kafka on EC2 instead of Amazon MSK

- Status: accepted · Date: 2026-11-18

## Context

The AWS account is a new Free plan account. Amazon MSK is only available on the Paid plan; every other
service the stack needs is available on the Free plan.

## Options

| Option | For | Against |
|---|---|---|
| Free plan, Kafka (KRaft) on one EC2 instance | Never billed beyond credits; real Kafka operations experience | Single broker; no "MSK" on the resume |
| Paid plan with MSK | Managed Kafka, "MSK" keyword | Charges beyond credits are real |

## Decision

Self-managed Kafka on EC2, provisioned by the `kafka` Terraform module.

## Consequences

The module owns listener config, security groups and the EBS volume. A single broker means no
replication; acceptable for a demo environment and stated in the README.
