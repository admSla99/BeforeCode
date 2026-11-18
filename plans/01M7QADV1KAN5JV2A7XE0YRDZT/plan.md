## Context
Card payments fail for some EU customers.
## Approach
Move checkout to Stripe Payment Intents.
## Affected areas
The checkout service.
## Out of scope
Refunds.
## Tasks
- Replace the charge call.
## Acceptance criteria
- AC-1: A card that needs 3-D Secure completes checkout.