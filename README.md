# distributed-workflow-engine-go

## Project Overview

This project is a small workflow service written in Go.  
It models order processing as a finite state machine and enforces state transitions using DynamoDB conditional writes.

The goal is simple: make state transitions correct under real-world conditions like retries, duplicate events, and concurrent updates.

It exposes a minimal HTTP API for transitions, but the focus is not the API layer. The focus is enforcing invariants at the domain and storage boundaries.

Core ideas demonstrated here:

- Explicit state modeling with an FSM
- Transition validation before mutation
- Optimistic locking using version checks
- Idempotent event handling
- Clear separation between domain logic, storage, and transport

---

## Problem Statement

In distributed systems, things retry. Messages get duplicated. Two instances process the same request at the same time.

A lot of systems treat “status” as just another string field in a database. That works until:

- A request is retried
- Two updates race each other
- An invalid transition slips through
- An event is processed twice

Once that happens, your data is inconsistent and your workflow is broken.

This project explores a stricter approach:

- Model the workflow explicitly.
- Reject invalid transitions.
- Enforce correctness at the storage layer.
- Make event processing idempotent.

The idea is not complexity. The idea is control.

---

## High-Level Architecture

The service is intentionally simple. Each layer has a clear responsibility.

- **Client** – Sends transition requests.
- **HTTP API** – Validates input and delegates to the domain layer.
- **FSM Layer** – Defines allowed transitions and rejects invalid ones.
- **DynamoDB** – Acts as the source of truth. Conditional writes enforce version checks and expected state.
- **Event Processing** – Handles domain events triggered by transitions.
- **Idempotency Table** – Records processed events to prevent duplicate execution.


Client → HTTP API → FSM → DynamoDB
↓
Event Processing → Idempotency Table


DynamoDB is not just storage here. It is part of the correctness boundary.  
Conditional writes ensure that concurrent updates cannot silently overwrite each other.

---

## State Machine Overview

Orders move through a defined lifecycle:

- Created
- Paid
- Packed
- Shipped
- Delivered
- Cancelled

Allowed transitions:


Created → Paid → Packed → Shipped → Delivered
↓
Cancelled


Any transition outside these paths is rejected.

State changes are only applied if:
- The current state matches what is expected
- The version matches (optimistic locking)

Invalid transitions and concurrent conflicts fail fast.

---

## Running Locally

- Go 1.21+
- DynamoDB Local (via Docker)

Setup instructions will be added as the implementation progresses.