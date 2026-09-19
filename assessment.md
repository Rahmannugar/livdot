Product Context - LIV DOT
LIV DOT is a platform for hosting and managing live, ticketed events.
Hosts create events and stream them to viewers who purchase access. Some events rely on production crews who handle the technical setup and operation of the broadcast.
The platform therefore supports workflows involving:
event creation and management


coordinating production crews for events


viewer ticket purchase and access to live streams


operational handling of issues such as stream failures or refund requests
Because these events happen live and involve paid access, the system must clearly communicate system state, readiness, and failure conditions to its users.

Assessment Overview
This exercise is designed to evaluate how you reason about backend systems that support high-trust workflows.
LIV DOT is building a platform where backend correctness directly affects:
paid access to live events
refunds and payouts
event lifecycle transitions
operational and administrative decisions
Because of this, we are particularly interested in how you think about system integrity, failure handling, and engineering tradeoffs.
The goal of this exercise is not to produce a perfect production design. Instead, we want to understand how you:
structure backend workflows
reason about state transitions and invariants
handle financial and operational edge cases
identify failure scenarios
explain technical decisions clearly
Concise explanations and simple diagrams are perfectly acceptable.
 
Expected Effort
This is a short backend system design exercise.
Expected effort: 2–4 hours
We are primarily interested in structured thinking, correctness, and practical engineering judgment rather than a polished specification.
 
Assessment Task
Design the backend subsystem for a paid live event workflow on LIV DOT.
The workflow includes the following scenario:
A host creates a paid event
Viewers purchase access
A production crew is assigned
The stream goes live
Refunds may occur if the stream fails before the defined threshold
Payouts occur after the event completes
For this exercise, assume the following rule:
Refund rule:
If the stream fails before 25% of the scheduled event duration, viewers are eligible for a full refund. After that threshold, refunds are not automatic and require admin review.
Your design should describe how the backend supports this workflow reliably.
Please address the following areas:
Core data model – main entities involved in the workflow
Key API or service operations – endpoints that support the workflow
Workflow and event state transitions
Payment, refund, and payout logic
Idempotency and retry handling
Failure scenarios and system safeguards
 
Deliverables
Please submit a short document containing the following.
1. Architecture / Workflow Overview
Briefly describe how the backend supports the end-to-end event flow.
This should include:
event creation
ticket purchase and entitlement
crew assignment
transition to live event
refund eligibility logic
payout readiness after completion
 
2. Data Model
Provide a simple representation of the main entities involved.
Examples may include:
Event
Booking / Crew Assignment
Ticket or Entitlement
Payment Transaction
Refund
Payout
Ledger Entry
You may present this as:
a simple schema diagram
a structured list of entities
a small relational model
 
3. API / Service Design
List the key backend endpoints or service operations needed to support the workflow.
High-level descriptions are sufficient.
 
4. State and Integrity Logic
Describe:
the event lifecycle states
key workflow transitions
important system invariants
Examples:
when refunds are allowed
when payouts are blocked
when viewer access is granted or revoked
 
5. Failure and Idempotency Thinking
Briefly discuss how the system should handle:
duplicate payment or webhook events
retry behavior
payout protection
failure cases that could create inconsistent state
Optional code snippets may be included if they help illustrate key logic (e.g., idempotency handling or refund eligibility rules), but a full implementation is not required.
 
 
Submission Format
Please submit your work as either:
a Google Drive folder link, or
a single PDF document
Recommended length: 2–5 pages
Ensure the link is accessible to reviewers.
 
Evaluation Criteria
Submissions will be evaluated based on:
clarity of workflow and state transitions
understanding of financial and transactional integrity
explicit handling of refund and payout rules
awareness of idempotency, retries, and webhook duplication
clarity of system invariants and failure handling
explanation of tradeoffs and implementation choices
We value clear reasoning and production realism over complexity.
 
